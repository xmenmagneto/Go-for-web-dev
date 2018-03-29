package main

import (
	"net/http"
	"database/sql"
	_"github.com/mattn/go-sqlite3"
	"gopkg.in/gorp.v1"

	"encoding/json"
	"net/url"
	"io/ioutil"
	"encoding/xml"
	"strconv"
	"golang.org/x/crypto/bcrypt"

	//"github.com/codegangsta/negroni"
	"github.com/urfave/negroni"
	"github.com/goincremental/negroni-sessions"
	"github.com/goincremental/negroni-sessions/cookiestore"
	"github.com/yosssi/ace"
	gmux "github.com/gorilla/mux"
)

type Book struct {
	PK int64 `db:"pk"`
	Title string `db:"title"`
	Author string `db:"author"`
	Classification string `db:"classification"`
	ID string `db:"id"`
	User string `db:"user"`
}

type User struct {
	Username string `db:"username"`
	Secret []byte `db:"secret"`
}

type Page struct {
	Books []Book
	Filter string
	User string  //let UI know which user logged in
}

type SearchResult struct {
	Title string `xml:"title,attr"`
	Author string `xml:"author,attr"`
	Year string `xml:"hyr,attr"`
	ID string `xml:"owi,attr"`
}

type ClassifySearchResponse struct {
	Results []SearchResult `xml:"works>work"`
}

type ClassifyBookResponse struct {
	BookData struct {
		Title string `xml:"title,attr"`
		Author string `xml:"author,attr"`
		ID string `xml:"owi,attr"`
	} `xml:"work"`
	Classification struct {
		MostPopular string `xml:"sfa,attr"`
	} `xml:"recommendations>ddc>mostPopular"`
}

var db *sql.DB
var dbmap *gorp.DbMap

func initDb() {
	db, _ = sql.Open("sqlite3", "dev.db")

	dbmap = &gorp.DbMap{Db: db, Dialect: gorp.SqliteDialect{}}

	dbmap.AddTableWithName(Book{}, "books").SetKeys(true,"pk")
	dbmap.AddTableWithName(User{}, "users").SetKeys(false,"username")
	dbmap.CreateTablesIfNotExists()
}

//  middleware to check database
func verifyDatabase(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if err := db.Ping(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	next(w, r)
}

//  middleware to check user session is always set before allowing users to enter main page
func verifyUser(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if r.URL.Path == "/login" {  // avoid redirect loop
		next(w, r)
		return
	}
	if username := getStringFromSession(r, "User"); username != "" {
		if user, _ := dbmap.Get(User{}, username); user != nil {
			next(w, r)
			return
		}
	}
	http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
}

//  implement initial sort
func getBookCollections(books *[]Book, sortCol string, filterByClass string, w http.ResponseWriter) bool {
	if sortCol == "" {
		sortCol = "pk" //set to default sorting by PK
	}
	var where string
	if filterByClass == "fiction" {
		where = " where classification between '800' and '900'"
	} else if filterByClass == "nonfiction" {
		where = " where classification not between '800' and '900'"
	}

	if _, err := dbmap.Select(books, "select * from books" + where + " order by " + sortCol); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	return true
}

func getStringFromSession(r *http.Request, key string) string {
	var strVal string
	//  get preference from session
	if val := sessions.GetSession(r).Get(key); val != nil {
		strVal = val.(string)
	}
	return strVal
}

type LoginPage struct {
	Error string
}
func main() {
	initDb()
	mux := gmux.NewRouter()

	//  login route
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		var p LoginPage
		if r.FormValue("register") != "" {
			secret, _ := bcrypt.GenerateFromPassword([]byte(r.FormValue("password")), bcrypt.DefaultCost)
			user := User{r.FormValue("username"), secret}
			if err := dbmap.Insert(&user); err != nil {
   				p.Error = err.Error()
			} else { // register successfully
				sessions.GetSession(r).Set("User", user.Username)
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
		} else if r.FormValue("login") != "" {
			user, err := dbmap.Get(User{}, r.FormValue("username"))
			if err != nil {
				p.Error = err.Error()
			} else if user == nil {
				p.Error = "No such user found with Username: " + r.FormValue("username")
			} else {
				//perform hard cast on the object returned from database
				u := user.(*User)
				if err = bcrypt.CompareHashAndPassword(u.Secret, []byte(r.FormValue("password"))); err != nil {
					p.Error = err.Error()
				} else {  //authenticated successfully
					sessions.GetSession(r).Set("User", u.Username)
					http.Redirect(w, r, "/", http.StatusFound)
					return
				}
			}
		}
		template, err := ace.Load("templates/login", "", nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err = template.Execute(w, p); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	//  logout route
	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		sessions.GetSession(r).Set("User", nil)
		sessions.GetSession(r).Set("Filter", nil)  //clear filter preference

		http.Redirect(w, r, "/login", http.StatusFound)
	})

	//  filter route
	mux.HandleFunc("/books", func(w http.ResponseWriter, r *http.Request) {
		var b []Book
		//  pass default sort preference to sort
		if !getBookCollections(&b, getStringFromSession(r, "sortBy"), r.FormValue("filter"), w) {
			return
		}

		//  store the sort preference in session
		sessions.GetSession(r).Set("Filter", r.FormValue("filter"))
		if err := json.NewEncoder(w).Encode(b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	}).Methods("GET").Queries("filter", "{filter:all|fiction|nonfiction}")

	//  sort route
	mux.HandleFunc("/books", func(w http.ResponseWriter, r *http.Request) {
		var b []Book
		if !getBookCollections(&b, r.FormValue("sortBy"), getStringFromSession(r, "Filter"), w) {
			return
		}

		//  store the sort preference in session
		sessions.GetSession(r).Set("SortBy", r.FormValue("sortBy"))
		if err := json.NewEncoder(w).Encode(b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	}).Methods("GET").Queries("sortBy", "{sortBy:title|author|classification}")

	//  root route
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		template, err := ace.Load("templates/index", "",nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}



		p := Page{Books: []Book{}, Filter: getStringFromSession(r, "Filter"), User: getStringFromSession(r, "User")}
		//  sort the book collection by sorting preference from session
		if !getBookCollections(&p.Books, getStringFromSession(r, "sortBy"), getStringFromSession(r, "Filter"), w) {
			return
		}


		//rows, err := db.Query("select pk,title,author,classification from books")
		//if err != nil {
		//	fmt.Print(err)
		//}
		//
		//for rows.Next() {
		//	var b Book
		//	rows.Scan(&b.PK, &b.Title, &b.Author, &b.Classification)
		//	p.Books = append(p.Books, b)
		//}

		if err = template.Execute(w, p); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	}).Methods("GET")


	//  search books route
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request){
		var results []SearchResult
		var err error

		if results, err = search(r.FormValue("search")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		encoder := json.NewEncoder(w)
		if err := encoder.Encode(results); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	}).Methods("POST")


	//  add book route
	mux.HandleFunc("/books", func(w http.ResponseWriter, r *http.Request) {
		var book ClassifyBookResponse
		var err error

		if book, err = find(r.FormValue("id")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}



		//result, err := db.Exec("insert into books (pk, title, author, id, classification) values (?, ?, ?, ?, ?)",
		//						nil, book.BookData.Title, book.BookData.Author, book.BookData.ID,
		//							book.Classification.MostPopular)
		//if err != nil {
		//	http.Error(w, err.Error(), http.StatusInternalServerError)
		//}
		//
		//pk, _ := result.LastInsertId()
		b := Book{
			PK: -1, //gorp will populate this value once the book is inserted into database
			Title: book.BookData.Title,
			Author: book.BookData.Author,
			Classification: book.Classification.MostPopular,
			ID: r.FormValue("id"),
			User: getStringFromSession(r, "User"),
		}
		//insert and populate b
		if dbmap.Insert(&b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := json.NewEncoder(w).Encode(b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}).Methods("PUT")

	//  delete book route
	mux.HandleFunc("/books/{pk}", func(w http.ResponseWriter, r *http.Request) {
		pk, _ := strconv.ParseInt(gmux.Vars(r)["pk"], 10, 64)
		//only allowed to delete books that belong to the user
		var b Book
		if err := dbmap.SelectOne(&b, "select * from books where pk=? and user=?", pk, getStringFromSession(r, "User")); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		if _, err := dbmap.Delete(&b); err != nil {

		}
		//if _, err := db.Exec("delete from books where pk=?", gmux.Vars(r)["pk"]); err != nil {
		//	http.Error(w, err.Error(), http.StatusInternalServerError)
		//	return
		//}
		w.WriteHeader(http.StatusOK)
	}).Methods("DELETE")

	n := negroni.Classic()
	n.Use(sessions.Sessions("go-for-web-dev", cookiestore.New([]byte("my-secret-123"))))
	n.Use(negroni.HandlerFunc(verifyDatabase))
	n.Use(negroni.HandlerFunc(verifyUser))
	n.UseHandler(mux)
	n.Run(":8080")
	//fmt.Println(http.ListenAndServe(":8080", nil))
}

func find(id string) (ClassifyBookResponse, error) {
	var c ClassifyBookResponse
	body, err := classifyAPI("http://classify.oclc.org/classify2/Classify?summary=true&owi=" + url.QueryEscape(id))

	if err != nil {
		return ClassifyBookResponse{}, err
	}

	err = xml.Unmarshal(body, &c)
	return c, err
}


func search(query string) ([]SearchResult, error) {
	var c ClassifySearchResponse
	body, err := classifyAPI("http://classify.oclc.org/classify2/Classify?summary=true&title=" + url.QueryEscape(query))

	if err != nil {
		return []SearchResult{}, err
	}

	err = xml.Unmarshal(body, &c)
	return c.Results, err
}

func classifyAPI(url string) ([]byte, error) {
	var resp *http.Response
	var err error

	if resp, err = http.Get(url); err != nil {
		return []byte{}, err
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)
}