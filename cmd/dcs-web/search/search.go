// vim:ts=4:sw=4:noexpandtab
package search

import (
	"dcs/ranking"
	"encoding/json"
	"flag"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

var indexBackends *string = flag.String("index_backends", "localhost:28081", "Index backends")
var packageLocation *regexp.Regexp = regexp.MustCompile(`debian-source-mirror/unpacked/([^/]+)_`)
var templates = template.Must(template.ParseFiles("templates/results.html"))

// This Match data structure is filled when receiving the match from the source
// backend. It is then enriched with the ranking of the corresponding path and
// displayed to the user.
type Match struct {
	// These members are filled in by the source backend.
	// NB: I would love to use dcs/regexp.Match as an anonymous struct here,
	// but encoding/json has a bug: It ignores anonymous struct fields when
	// decoding/encoding (it’s documented :-/).
	Path    string
	Line    int
	Context string
	Ranking float32

	// These are filled in by Prettify()
	SourcePackage string
	RelativePath string

	// The ranking of the ResultPath corresponding to Path
	PathRanking float32
}

func (m *Match) Prettify() {
	index := packageLocation.FindStringSubmatchIndex(m.Path)
	if index == nil {
		log.Fatalf("Invalid path in result: %s", m.Path)
	}

	m.SourcePackage = m.Path[index[2]:index[3]]
	m.RelativePath = m.Path[index[3]:]
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

func sendIndexQuery(query url.URL, backend string, indexResults chan string, done chan bool) {
	t0 := time.Now()
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

	t1 := time.Now()

	log.Printf("[%s] %d results in %v\n", backend, len(files), t1.Sub(t0))

	for _, filename := range files {
		indexResults <- filename
	}
	done <- true
}

// TODO: refactor this with sendIndexQuery.
func sendSourceQuery(query url.URL, filenames []ranking.ResultPath, matches chan Match, done chan bool) {
	query.Scheme = "http"
	// TODO: make this configurable
	query.Host = "localhost:28082"
	query.Path = "/source"
	q := query.Query()
	q.Set("limit", "40")
	query.RawQuery = q.Encode()
	v := url.Values{}
	cnt := 0
	for _, filename := range filenames {
		if cnt > 40 * 10 {
			break
		}
		cnt++
		v.Add("filename", filename.Path)
	}
	log.Printf("(source) asking %s (with %d filenames)\n", query.String(), len(filenames))
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

func Search(w http.ResponseWriter, r *http.Request) {
	var t0, t1, t2, t3, t4 time.Time
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

	var files ranking.ResultPaths
	// We also keep the files in a map with their path as the key so that we
	// can correlate a match to a (ranked!) filename later on.
	fileMap := make(map[string]ranking.ResultPath)
	for i := 0; i < len(backends); {
		select {
		case path := <-indexResults:
			// Time to the first result (≈ time to query the regexp index in
			// case len(backends) == 1)
			if t1.IsZero() {
				t1 = time.Now()
			}
			result := ranking.ResultPath{path, [2]int{0, 0}, 0}
			result.Rank()
			files = append(files, result)

		case <-done:
			i++
		}
	}

	// Time to receive and rank the results
	t2 = time.Now()

	// TODO: for the same ranking, this sort needs to be stable over multiple
	// queries! we could use the filename to ensure this. otherwise pagination
	// doesn’t work.
	sort.Sort(files)

	// Time to sort the results
	t3 = time.Now()

	// XXX: Here we make an educated guess about how many top-ranked
	// (currently) search results should be considered for further ranking and
	// then source-querying. Obviously, this makes the search less correct, but
	// the vast majority of people wouldn’t even notice. Maybe we could expose
	// a &pedantic=yes parameter for people who care about correct searches
	// (then again, this offers potential for a DOS attack).
	relevantFiles := files[:1000]

	for _, result := range relevantFiles {
		sourcePkgName := result.Path[result.SourcePkgIdx[0]:result.SourcePkgIdx[1]]
		result.Ranking *= querystr.Match(&result.Path)
		result.Ranking *= querystr.Match(&sourcePkgName)
		fileMap[result.Path] = result
	}

	sort.Sort(relevantFiles)

	// TODO: Essentially, this follows the MapReduce pattern. Our input is the
	// filename list, we map that onto matches in the first step (remote), then
	// we map rankings onto it and then we need it sorted. Maybe there is some
	// Go package already which can help us here?

	// NB: At this point we could implement some kind of scheduler in the
	// future to split the load between multiple source servers (that might
	// even be multiple instances on the same machine just serving from
	// different disks).
	matches := make(chan Match)
	go sendSourceQuery(*query, relevantFiles, matches, done)

	var results SearchResults
	for i := 0; i < 1; {
		select {
		case match := <-matches:
			// Time to the first index result
			if t4.IsZero() {
				t4 = time.Now()
			}
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
	err := templates.ExecuteTemplate(w, "results.html", map[string]interface{} {
		"results": results,
		"t0": t1.Sub(t0),
		"t1": t2.Sub(t1),
		"t2": t3.Sub(t2),
		"t3": t4.Sub(t3),
		"numfiles": len(files),
		"numresults": len(results),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
