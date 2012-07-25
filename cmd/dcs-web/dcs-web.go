// vim:ts=4:sw=4:noexpandtab
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
)

var indexBackends *string = flag.String("index_backends", "localhost:28081", "Index backends")

func Index(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `<html><form action="/search" method="get"><input type="text" name="q"><input type="submit">`)
}

func sendIndexQuery(query url.URL, backend string, indexResults chan string, done chan bool) {
	query.Scheme = "http"
	query.Host = backend
	query.Path = "/index"
	log.Printf("asking " + query.String())
	resp, err := http.Get(query.String())
	if err != nil {
		done <- true
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		done <- true
		return
	}

	var files []string
	if err := json.Unmarshal(body, &files); err != nil {
		// TODO: Better error message
		log.Printf("Invalid result from backend " + backend)
		done <- true
		return
	}

	log.Printf("files = %s\n", files)

	for _, filename := range files {
		indexResults <- filename
	}
	done <- true
}

func Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL
	log.Printf(`Search query for "` + query.String() + `"`)

	// TODO: compile the regular expression right here so that we don’t do it N
	// times and can properly error out.

	// Send the query to all index backends (our index is sharded into multiple
	// pieces).
	backends := strings.Split(*indexBackends, ",")
	done := make(chan bool)
	indexResults := make(chan string)
	for _, backend := range backends {
		log.Printf("Sending query to " + backend)
		go sendIndexQuery(*query, backend, indexResults, done)
	}

	fmt.Fprintf(w, `<html>Results:<ul>`)
	for i := 0; i < len(backends); {
		select {
		case result := <-indexResults:
			fmt.Printf("Got a result: %s\n", result)
			fmt.Fprintf(w, `<li>%s</li>`, result)
			// TODO: we need a sorted data structure in which we can insert the filename

		case <-done:
			i++
		}
	}

	// TODO: We got all the results, now rank them, keep the top 10, then query
	// all the source code files.

	fmt.Fprintf(w, `</ul>`)
}

func main() {
	flag.Parse()
	fmt.Println("Debian Code Search webapp")

	http.HandleFunc("/", Index)
	http.HandleFunc("/search", Search)
	log.Fatal(http.ListenAndServe(":28080", nil))
}
