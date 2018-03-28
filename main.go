package main

import (
	"net/http"
	"database/sql"
	_"github.com/mattn/go-sqlite3"

	"encoding/json"
	"net/url"
	"io/ioutil"
	"encoding/xml"

	"github.com/codegangsta/negroni"
	"github.com/yosssi/ace"
	"fmt"
)

type Book struct {
	PK int
	Title string
	Author string
	Classification string
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

//  middleware to check database
func verifyDababase(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if err := db.Ping(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	next(w, r)
}

func main() {
	db, _ = sql.Open("sqlite3", "dev.db")

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		template, err := ace.Load("templates/index", "",nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		p := Page{Books: []Book{}}
		//  fetch all books in the database
		rows, err := db.Query("select pk,title,author,classification from books")
		if err != nil {
			fmt.Print(err)
		}

		for rows.Next() {
			var b Book
			rows.Scan(&b.PK, &b.Title, &b.Author, &b.Classification)
			p.Books = append(p.Books, b)
		}

		if err = template.Execute(w, p); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	})

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

	})


	mux.HandleFunc("/books/add", func(w http.ResponseWriter, r *http.Request) {
		var book ClassifyBookResponse
		var err error

		if book, err = find(r.FormValue("id")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		_, err = db.Exec("insert into books (pk, title, author, id, classification) values (?, ?, ?, ?, ?)",
								nil, book.BookData.Title, book.BookData.Author, book.BookData.ID,
									book.Classification.MostPopular)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

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