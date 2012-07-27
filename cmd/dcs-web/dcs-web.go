// vim:ts=4:sw=4:noexpandtab
package main

import (
	"database/sql"
	_ "github.com/jbarham/gopgsqldriver"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var indexBackends *string = flag.String("index_backends", "localhost:28081", "Index backends")
var packageLocation *regexp.Regexp = regexp.MustCompile(`debian-source-mirror/unpacked/([^/]+)_`)
var db sql.DB
var rankQuery *sql.Stmt

// The regular expression trigram index provides us a path to a potential
// result. This data structure represents such a path and allows for ranking
// and sorting each path.
type ResultPath struct {
	Path string
	Ranking float32
}

func (rp *ResultPath) Rank() {
	m := packageLocation.FindStringSubmatch(rp.Path)
	if len(m) != 2 {
		log.Fatal("Invalid path in result: %s", rp.Path)
	}

	log.Printf("should rank source package %s", m[1])
	rows, err := rankQuery.Query(m[1])
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	rows.Next()
	var ranking float32
	if err = rows.Scan(&ranking); err != nil {
		log.Fatal(err)
	}
	rp.Ranking = ranking
	log.Printf("ranking = %f", ranking)
}

type ResultPaths []ResultPath

func (r ResultPaths) Len() int {
	return len(r)
}

func (r ResultPaths) Less(i, j int) bool {
	return r[i].Ranking > r[j].Ranking
}

func (r ResultPaths) Swap(i, j int) {
	r[i], r[j] = r[j], r[i]
}


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
func sendSourceQuery(query url.URL, filenames []ResultPath, matches chan Match, done chan bool) {
	query.Scheme = "http"
	// TODO: make this configurable
	query.Host = "localhost:28082"
	query.Path = "/source"
	v := url.Values{}
	for _, filename := range filenames {
		v.Add("filename", filename.Path)
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

	var files ResultPaths
	fmt.Fprintf(w, `<html>Results:<ul>`)
	for i := 0; i < len(backends); {
		select {
		case path := <-indexResults:
			fmt.Printf("Got a result: %s\n", path)
			result := ResultPath{path, 0}
			result.Rank()
			files = append(files, result)
			// TODO: we need a sorted data structure in which we can insert the filename

		case <-done:
			i++
		}
	}

	sort.Sort(files)

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

	db, err := sql.Open("postgres", "dbname=dcs")
	if err != nil {
		log.Fatal(err)
	}

	rankQuery, err = db.Prepare("SELECT popcon FROM pkg_ranking WHERE package = $1")
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/", Index)
	http.HandleFunc("/search", Search)
	log.Fatal(http.ListenAndServe(":28080", nil))
}
