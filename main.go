package main

import (
	"fmt"
	"net/http"
	"html/template"
	"database/sql"
	_"github.com/mattn/go-sqlite3"

	"encoding/json"
	"net/url"
	"io/ioutil"
	"encoding/xml"
)

type Page struct {
	Name string
	DBStatus bool
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

func main() {
	templates := template.Must(template.ParseFiles("templates/index.html"))

	db, _ := sql.Open("sqlite3", "dev.db")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := Page{Name: "Gopher"}
		if name := r.FormValue("name"); name != "" {
			p.Name = name
		}

		p.DBStatus = db.Ping() == nil

		if err := templates.ExecuteTemplate(w,"index.html", p); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	})

	http.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request){
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

	fmt.Println(http.ListenAndServe(":8080", nil))
}


func search(query string) ([]SearchResult, error) {
	var resp *http.Response
	var err error

	if resp, err = http.Get("http://classify.oclc.org/classify2/Classify?&summary=true&title=" + url.QueryEscape(query)); err != nil {
		return []SearchResult{}, err
	}

	defer resp.Body.Close()
	var body []byte
	if body, err = ioutil.ReadAll(resp.Body); err != nil {
		return []SearchResult{}, err
	}

	var c ClassifySearchResponse
	err = xml.Unmarshal(body, &c)
	return c.Results, err
}