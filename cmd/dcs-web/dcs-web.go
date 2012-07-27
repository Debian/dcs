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
	"sort"
	"strings"
)

var indexBackends *string = flag.String("index_backends", "localhost:28081", "Index backends")

type Match struct {
	Path    string
	Line    int
	Ranking int
	Context string
}

// This type implements sort.Interface so that we can sort it by rank.
type SearchResults []Match

func (s SearchResults) Len() int {
	return len(s)
}

func (s SearchResults) Less(i, j int) bool {
	return s[i].Ranking < s[j].Ranking
}

func (s SearchResults) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func Index(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `<html><form action="/search" method="get"><input type="text" name="q"><input type="submit">`)
}

func sendIndexQuery(query url.URL, backend string, indexResults chan string, done chan bool) {
	query.Scheme = "http"
	query.Host = backend
	query.Path = "/index"
	log.Printf("asking %s\n", query.String())
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

// TODO: refactor this with sendIndexQuery.
func sendSourceQuery(query url.URL, filenames []string, matches chan Match, done chan bool) {
	query.Scheme = "http"
	// TODO: make this configurable
	query.Host = "localhost:28082"
	query.Path = "/source"
	v := url.Values{}
	for _, filename := range filenames {
		v.Add("filename", filename)
	}
	log.Printf("(source) asking %s\n", query.String())
	resp, err := http.PostForm(query.String(), v)
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

	var beMatches []Match
	if err := json.Unmarshal(body, &beMatches); err != nil {
		log.Printf("Invalid result from backend (source)")
		done <- true
		return
	}

	for _, match := range beMatches {
		matches <- match
	}

	done <- true
}

func (m *Match) Rank() {
	// TODO: compute a rank
	m.Ranking = 3
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

	var files []string
	fmt.Fprintf(w, `<html>Results:<ul>`)
	for i := 0; i < len(backends); {
		select {
		case result := <-indexResults:
			fmt.Printf("Got a result: %s\n", result)
			files = append(files, result)
			// TODO: we need a sorted data structure in which we can insert the filename

		case <-done:
			i++
		}
	}

	// TODO: Essentially, this follows the MapReduce pattern. Our input is the
	// filename list, we map that onto matches in the first step (remote), then
	// we map rankings onto it and then we need it sorted. Maybe there is some
	// Go package already which can help us here?

	// TODO: We got all the results, now rank them, keep the top 10, then query
	// all the source code files. The problem here is that, depending on how we
	// do the ranking, we need all the precise matches first :-/.

	// NB: At this point we could implement some kind of scheduler in the
	// future to split the load between multiple source servers (that might
	// even be multiple instances on the same machine just serving from
	// different disks).
	matches := make(chan Match)
	go sendSourceQuery(*query, files, matches, done)

	var results SearchResults
	for i := 0; i < 1; {
		select {
		case match := <-matches:
			match.Rank()
			results = append(results, match)
		case <-done:
			i++
		}
	}

	sort.Sort(results)
	for _, result := range results {
		fmt.Fprintf(w, `<li><code>%s</code>:%d<br><code>%s</code></li>`, result.Path, result.Line, result.Context)
	}

	fmt.Fprintf(w, `</ul>`)
}

func main() {
	flag.Parse()
	fmt.Println("Debian Code Search webapp")

	http.HandleFunc("/", Index)
	http.HandleFunc("/search", Search)
	log.Fatal(http.ListenAndServe(":28080", nil))
}
