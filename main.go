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

	"github.com/codegangsta/negroni"
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
func verifyDababase(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if err := db.Ping(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	next(w, r)
}

func main() {
	initDb()
	mux := gmux.NewRouter()

	//  sort route
	mux.HandleFunc("/books", func(w http.ResponseWriter, r *http.Request) {
		columnName := r.FormValue("sortBy")
		if columnName != "title" && columnName != "author" && columnName != "classification" {
			http.Error(w, "Invalid column name", http.StatusInternalServerError)
			return
		}

		var b []Book
		if _, err := dbmap.Select(&b, "select * from books order by " + columnName); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := json.NewEncoder(w).Encode(b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	}).Methods("GET")

	//  root route
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		template, err := ace.Load("templates/index", "",nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		p := Page{Books: []Book{}}
		//  fetch all books in the database
		if _, err := dbmap.Select(&p.Books, "select * from books"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
	n.Use(negroni.HandlerFunc(verifyDababase))
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