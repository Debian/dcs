// vim:ts=4:sw=4:noexpandtab
package main

import (
	"database/sql"
	_ "github.com/jbarham/gopgsqldriver"
	"dcs/ranking"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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

func (rp *ResultPath) Rank(query *ranking.QueryStr) {
	m := packageLocation.FindStringSubmatch(rp.Path)
	if len(m) != 2 {
		log.Fatal("Invalid path in result: %s", rp.Path)
	}

	//log.Printf("should rank source package %s", m[1])
	sourcePackage := m[1]
	rows, err := rankQuery.Query(sourcePackage)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	rows.Next()
	var inst, rdep float32
	if err = rows.Scan(&inst, &rdep); err != nil {
		log.Fatal(err)
	}
	rp.Ranking = inst * rdep * query.Match(&rp.Path) * query.Match(&sourcePackage)
	//log.Printf("ranking = %f", ranking)
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
	// These members are filled in by the source backend.
	Path    string
	Line    int
	Context string

	Ranking int

	// These are filled in by Prettify()
	SourcePackage string
	RelativePath string

	// The ranking of the ResultPath corresponding to Path
	PathRanking float32
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
	q := query.Query()
	q.Set("limit", "40")
	query.RawQuery = q.Encode()
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

func (m *Match) Prettify() {
	index := packageLocation.FindStringSubmatchIndex(m.Path)
	if index == nil {
		log.Fatal("Invalid path in result: %s", m.Path)
	}

	m.SourcePackage = m.Path[index[2]:index[3]]
	m.RelativePath = m.Path[index[3]:]
}

func Search(w http.ResponseWriter, r *http.Request) {
	var t0, t1, t2, t3 time.Time
	query := r.URL
	querystr := ranking.NewQueryStr(query.Query().Get("q"))
	log.Printf(`Search query for "` + query.String() + `"`)

	// TODO: compile the regular expression right here so that we don’t do it N
	// times and can properly error out.

	// Send the query to all index backends (our index is sharded into multiple
	// pieces).
	backends := strings.Split(*indexBackends, ",")
	done := make(chan bool)
	indexResults := make(chan string)
	t0 = time.Now()
	for _, backend := range backends {
		log.Printf("Sending query to " + backend)
		go sendIndexQuery(*query, backend, indexResults, done)
	}

	var files ResultPaths
	// We also keep the files in a map with their path as the key so that we
	// can correlate a match to a (ranked!) filename later on.
	fileMap := make(map[string]ResultPath)
	for i := 0; i < len(backends); {
		select {
		case path := <-indexResults:
			// Time to the first result (≈ time to query the regexp index in
			// case len(backends) == 1)
			t1 = time.Now()
			result := ResultPath{path, 0}
			result.Rank(&querystr)
			files = append(files, result)
			fileMap[path] = result

		case <-done:
			i++
		}
	}

	sort.Sort(files)

	// Time to rank and sort the results
	t2 = time.Now()

	// TODO: Essentially, this follows the MapReduce pattern. Our input is the
	// filename list, we map that onto matches in the first step (remote), then
	// we map rankings onto it and then we need it sorted. Maybe there is some
	// Go package already which can help us here?

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
			// Time to the first index result
			t3 = time.Now()
			match.Rank()
			match.Prettify()
			fileResult, ok := fileMap[match.Path]
			if !ok {
				log.Printf("Could not find %s in fileMap?!\n", match.Path)
			} else {
				match.PathRanking = fileResult.Ranking
			}
			results = append(results, match)
		case <-done:
			i++
		}
	}

	sort.Sort(results)
	t, _ := template.ParseFiles("templates/results.html")
	t.Execute(w, map[string]interface{} {
		"results": results,
		"t0": t1.Sub(t0),
		"t1": t2.Sub(t1),
		"t2": t3.Sub(t2),
		"numfiles": len(files),
		"numresults": len(results),
	})
}

func Show(w http.ResponseWriter, r *http.Request) {
	query := r.URL
	filename := query.Query().Get("file")
	line, err := strconv.ParseInt(query.Query().Get("line"), 10, 0)
	if err != nil {
		log.Printf("%v\n", err)
		return
	}
	log.Printf("Showing file %s, line %d\n", filename, line)

	// TODO: path configuration
	file, err := os.Open(`/media/sdg/debian-source-mirror/unpacked/` + filename)
	if err != nil {
		log.Printf("%v\n", err)
		return
	}
	defer file.Close()

	contents, err := ioutil.ReadAll(file)
	if err != nil {
		log.Printf("%v\n", err)
		return
	}

	t, _ := template.ParseFiles("templates/show.html")
	t.Execute(w, map[string]interface{} {
		// XXX: Has string(contents) any problems when the file is not valid UTF-8?
		// (while the indexer only cares for UTF-8, an attacker could send us any file path)
		"contents": string(contents),
	})
}

func main() {
	flag.Parse()
	fmt.Println("Debian Code Search webapp")

	db, err := sql.Open("postgres", "dbname=dcs")
	if err != nil {
		log.Fatal(err)
	}

	rankQuery, err = db.Prepare("SELECT popcon, rdepends FROM pkg_ranking WHERE package = $1")
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/", Index)
	http.HandleFunc("/search", Search)
	http.HandleFunc("/show", Show)
	log.Fatal(http.ListenAndServe(":28080", nil))
}
