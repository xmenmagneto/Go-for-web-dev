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
}

type Page struct {
	Books []Book
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

func main() {
	initDb()
	mux := gmux.NewRouter()

	//  filter route
	mux.HandleFunc("/books", func(w http.ResponseWriter, r *http.Request) {
		var b []Book
		//  pass default sort preference to sort
		if !getBookCollections(&b, getStringFromSession(r, "sortBy"), w) {
			return
		}

		//  store the sort preference in session
		sessions.GetSession(r).Set("SortBy", r.FormValue("sortBy"))
		if err := json.NewEncoder(w).Encode(b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	}).Methods("GET").Queries("filter", "{filter:all|fiction|nonfiction}")

	//  sort route
	mux.HandleFunc("/books", func(w http.ResponseWriter, r *http.Request) {
		var b []Book
		if !getBookCollections(&b, r.FormValue("sortBy"), w) {
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



		p := Page{Books: []Book{}}
		//  sort the book collection by sorting preference from session
		if !getBookCollections(&p.Books, getStringFromSession(r, "sortBy"), w) {
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
		if _, err := dbmap.Delete(&Book{pk, "", "", "", ""}); err != nil {

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