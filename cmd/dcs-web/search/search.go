// vim:ts=4:sw=4:noexpandtab
package search

import (
	"bytes"
	"dcs/cmd/dcs-web/common"
	"dcs/ranking"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

var indexBackends *string = flag.String("index_backends", "localhost:28081", "Index backends")
var timingTotalPath *string = flag.String("timing_total_path", "", "path to a file to save timing data (total request time)")
var timingFirstRegexp *string = flag.String("timing_first_regexp", "", "path to a file to save timing data (first regexp)")
var timingFirstIndex *string = flag.String("timing_first_index", "", "path to a file to save timing data (first index)")
var timingReceiveRank *string = flag.String("timing_receive_rank", "", "path to a file to save timing data (receive and rank)")
var timingSort *string = flag.String("timing_sort", "", "path to a file to save timing data (sort)")
var tTotal *os.File
var tFirstRegexp *os.File
var tFirstIndex *os.File
var tReceiveRank *os.File
var tSort *os.File
var requestCounter int64 = 0

type SourceReply struct {
	// The number of the last used filename, needed for pagination
	LastUsedFilename int

	AllMatches []Match
}

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
	RelativePath  string

	// The ranking of the ResultPath corresponding to Path
	PathRanking float32

	// The combined Ranking * PathRanking
	FinalRanking float32
}

func (m *Match) Prettify() {
	for i := 0; i < len(m.Path); i++ {
		if m.Path[i] == '_' {
			m.SourcePackage = m.Path[:i]
			m.RelativePath = m.Path[i:]
			break
		}
	}
}

// This type implements sort.Interface so that we can sort it by rank.
type SearchResults []Match

func (s SearchResults) Len() int {
	return len(s)
}

func (s SearchResults) Less(i, j int) bool {
	if s[i].FinalRanking == s[j].FinalRanking {
		// On a tie, we use the path to make the order of results stable over
		// multiple queries (which can have different results depending on
		// which index backend reacts quicker).
		return s[i].Path > s[j].Path
	}
	return s[i].FinalRanking > s[j].FinalRanking
}

func (s SearchResults) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func OpenTimingFiles() (err error) {
	if len(*timingTotalPath) > 0 {
		tTotal, err = os.Create(*timingTotalPath)
		if err != nil {
			log.Fatal(err)
		}
	}

	if len(*timingFirstRegexp) > 0 {
		tFirstRegexp, err = os.Create(*timingFirstRegexp)
		if err != nil {
			log.Fatal(err)
		}
	}

	if len(*timingFirstIndex) > 0 {
		tFirstIndex, err = os.Create(*timingFirstIndex)
		if err != nil {
			log.Fatal(err)
		}
	}

	if len(*timingReceiveRank) > 0 {
		tReceiveRank, err = os.Create(*timingReceiveRank)
		if err != nil {
			log.Fatal(err)
		}
	}

	if len(*timingSort) > 0 {
		tSort, err = os.Create(*timingSort)
		if err != nil {
			log.Fatal(err)
		}
	}

	return
}

// Convenience function to get a moving window over the result slice.
// Prevents out of bounds access.
func resultWindow(result []ranking.ResultPath, start int, length int) []ranking.ResultPath {
	end := start + length
	if end > len(result) {
		end = len(result)
	}
	return result[start:end]
}

func sendIndexQuery(query url.URL, backend string, indexResults chan ranking.ResultPath, done chan int, rankingopts ranking.RankingOpts) {
	t0 := time.Now()
	query.Scheme = "http"
	query.Host = backend
	query.Path = "/index"
	log.Printf("asking %s\n", query.String())
	resp, err := http.Get(query.String())
	if err != nil {
		done <- 1
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		done <- 1
		return
	}

	var files []string
	if err := json.Unmarshal(body, &files); err != nil {
		// TODO: Better error message
		log.Printf("Invalid result from backend " + backend)
		done <- 1
		return
	}

	t1 := time.Now()

	log.Printf("[%s] %d results in %v\n", backend, len(files), t1.Sub(t0))

	var result ranking.ResultPath
	for _, filename := range files {
		result.Path = filename
		result.Rank(&rankingopts)
		if result.Ranking > 0 {
			indexResults <- result
		}
	}
	done <- 1
}

// TODO: refactor this with sendIndexQuery.
func sendSourceQuery(query url.URL, values chan ranking.ResultPaths, cont chan bool, matches chan Match, done chan int, allResults bool, skip int) {

	// How many results per page to display tops.
	limit := 0
	if !allResults {
		limit = 40
	}

	query.Scheme = "http"
	// TODO: make this configurable
	query.Host = "localhost:28082"
	query.Path = "/source"
	q := query.Query()
	q.Set("limit", fmt.Sprintf("%d", limit))
	query.RawQuery = q.Encode()

	numMatches := 0
	lastUsedFilename := 0
	skipped := 0
	for {
		filenames, open := <-values
		if !open {
			// No more values? We have to abort.
			done <- 0
			return
		}
		start := skip
		fmt.Printf("start = %d, skip = %d, len(filenames) = %d\n", start, skip, len(filenames))
		for start < len(filenames) {
			v := url.Values{}
			for _, filename := range resultWindow(filenames, start, 500) {
				v.Add("filename", filename.Path)
			}
			log.Printf("(source) asking %s (with %d of %d filenames)\n", query.String(), len(v["filename"]), len(filenames))
			resp, err := http.PostForm(query.String(), v)
			if err != nil {
				done <- 0
				return
			}
			defer resp.Body.Close()

			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				done <- 0
				return
			}

			var reply SourceReply
			if err := json.Unmarshal(body, &reply); err != nil {
				log.Printf("Invalid result from backend (source)")
				done <- 0
				return
			}

			if len(reply.AllMatches) > 0 {
				lastUsedFilename = skipped + skip + reply.LastUsedFilename
				fmt.Printf("last used filename: %d -> %d\n", reply.LastUsedFilename, lastUsedFilename)
			}

			for _, match := range reply.AllMatches {
				matches <- match
				numMatches++
			}

			if numMatches >= limit {
				done <- lastUsedFilename
				cont <- false
				return
			}

			// If there were no matches, we provide more filenames and retry
			start += 500
		}
		skip -= len(filenames)
		skipped += len(filenames)
		if skip < 0 {
			done <- lastUsedFilename
			cont <- false
			return
		}
		cont <- true
	}

	done <- lastUsedFilename
	cont <- false
}

func Search(w http.ResponseWriter, r *http.Request) {
	var tinit, t0, t1, t2, t3, t4 time.Time

	tinit = time.Now()

	// Rewrite the query to extract words like "lang:c" from the querystring
	// and place them in parameters.
	rewritten := RewriteQuery(*r.URL)

	query := rewritten.Query()

	// Usage of this flag should be restricted to local IP addresses or
	// something like that (it causes a lot of load, but it makes analyzing the
	// search engine’s ranking easier).
	allResults := query.Get("all") == "1"

	// Users can configurable which ranking factors (with what weight) they
	// want to use. rankingopts stores these values, extracted from the query
	// parameters.
	rankingopts := ranking.RankingOptsFromQuery(query)

	querystr := ranking.NewQueryStr(query.Get("q"))

	// Number of files to skip when searching. Used for pagination.
	skip64, _ := strconv.ParseInt(query.Get("skip"), 10, 0)
	skip := int(skip64)

	log.Printf(`Search query for "` + rewritten.String() + `"`)
	log.Printf("opts: %v\n", rankingopts)

	log.Printf("Query parsed after %v\n", time.Now().Sub(tinit))

	// TODO: compile the regular expression right here so that we don’t do it N
	// times and can properly error out.

	// Send the query to all index backends (our index is sharded into multiple
	// pieces).
	backends := strings.Split(*indexBackends, ",")
	done := make(chan int)
	indexResults := make(chan ranking.ResultPath, 10)
	t0 = time.Now()
	for _, backend := range backends {
		log.Printf("Sending query to " + backend)
		go sendIndexQuery(rewritten, backend, indexResults, done, rankingopts)
	}

	var files ranking.ResultPaths
	// We also keep the files in a map with their path as the key so that we
	// can correlate a match to a (ranked!) filename later on.
	fileMap := make(map[string]ranking.ResultPath)
	for i := 0; i < len(backends); {
		select {
		case result := <-indexResults:
			// Time to the first result (≈ time to query the regexp index in
			// case len(backends) == 1)
			if t1.IsZero() {
				t1 = time.Now()
			}
			files = append(files, result)

		case <-done:
			i++
		}
	}

	// Time to receive and rank the results
	t2 = time.Now()
	log.Printf("All index backend results after %v\n", t2.Sub(t0))

	sort.Sort(files)

	// Time to sort the results
	t3 = time.Now()

	// Now we set up a goroutine which grabs 1000 filenames, ranks them and
	// sends them to sendSourceQuery until sendSourceQuery tells it to stop.
	// For most queries, the first batch will be enough, but for queries with a
	// high false-positive rate (that is, file does not contain the searched
	// word, but all trigrams), we need multiple iterations.
	values := make(chan ranking.ResultPaths)
	cont := make(chan bool)

	go func() {
		start := 0

		for start < len(files) {
			batch := ranking.ResultPaths(resultWindow(files, start, 1000))

			for idx, result := range batch {
				sourcePkgName := result.Path[result.SourcePkgIdx[0]:result.SourcePkgIdx[1]]
				if rankingopts.Pathmatch {
					batch[idx].Ranking *= querystr.Match(&result.Path)
				}
				if rankingopts.Sourcepkgmatch {
					batch[idx].Ranking *= querystr.Match(&sourcePkgName)
				}
				if rankingopts.Weighted {
					batch[idx].Ranking *= 0.2205 * querystr.Match(&result.Path)
					batch[idx].Ranking *= 0.0011 * querystr.Match(&sourcePkgName)
				}
				fileMap[result.Path] = batch[idx]
			}

			sort.Sort(batch)
			values <- batch
			if !<-cont {
				return
			}

			start += 1000
		}

		// Close the channel to signal that there are no more values available
		close(values)
	}()

	tBeforeSource := time.Now()

	// NB: At this point we could implement some kind of scheduler in the
	// future to split the load between multiple source servers (that might
	// even be multiple instances on the same machine just serving from
	// different disks).
	matches := make(chan Match)
	go sendSourceQuery(rewritten, values, cont, matches, done, allResults, skip)

	var results SearchResults
	var lastUsedFilename int
	maxPathRanking := float32(0)
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
			if match.PathRanking > maxPathRanking {
				maxPathRanking = match.PathRanking
			}
			results = append(results, match)
		case lastUsedFilename = <-done:
			i++
		}
	}

	fmt.Printf("All source backend results after %v\n", time.Now().Sub(tBeforeSource))

	// Now store the combined ranking of PathRanking (pre) and Ranking (post).
	// We add the values because they are both percentages.
	// To make the Ranking (post) less significant, we multiply it with
	// 1/10 * maxPathRanking
	for idx, match := range results {
		results[idx].FinalRanking = match.PathRanking + ((maxPathRanking * 0.1) * match.Ranking)
	}

	sort.Sort(results)

	// Add our measurements as HTTP headers so that we can log them in nginx.
	outHeader := w.Header()
	// time to first regexp result
	outHeader.Add("dcs-t0", fmt.Sprintf("%.2fms", float32(t1.Sub(t0).Nanoseconds())/1000/1000))
	// time to receive and rank
	outHeader.Add("dcs-t1", fmt.Sprintf("%.2fms", float32(t2.Sub(t1).Nanoseconds())/1000/1000))
	// time to sort
	outHeader.Add("dcs-t2", fmt.Sprintf("%.2fms", float32(t3.Sub(t2).Nanoseconds())/1000/1000))
	// time to first index result
	outHeader.Add("dcs-t3", fmt.Sprintf("%.2fms", float32(t4.Sub(t3).Nanoseconds())/1000/1000))
	// amount of regexp results
	outHeader.Add("dcs-numfiles", fmt.Sprintf("%.d", len(files)))
	// amount of source results
	outHeader.Add("dcs-numresults", fmt.Sprintf("%.d", len(results)))

	// NB: We send the template output to a buffer because that is faster. We
	// also just use the template for the header of the page and then print the
	// results directly from Go, which saves ≈ 10 ms (!).
	outputBuffer := new(bytes.Buffer)
	err := common.Templates.ExecuteTemplate(outputBuffer, "results.html", map[string]interface{}{
		//"results": results,
		"t0":         t1.Sub(t0),
		"t1":         t2.Sub(t1),
		"t2":         t3.Sub(t2),
		"t3":         t4.Sub(t3),
		"numfiles":   len(files),
		"numresults": len(results),
		"timing":     (rewritten.Query().Get("notiming") != "1"),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	outputBuffer.WriteTo(w)

	for _, result := range results {
		fmt.Fprintf(w, `<li><a href="/show?file=%s%s&amp;line=%d&amp;numfiles=%d#L%d"><code><strong>%s</strong>%s</code>:%d</a><br><code>%s</code><br>
PathRank: %g, Rank: %g, Final: %g</li>`,
			result.SourcePackage,
			result.RelativePath,
			result.Line,
			len(files),
			result.Line,
			result.SourcePackage,
			result.RelativePath,
			result.Line,
			result.Context,
			result.PathRanking,
			result.Ranking,
			result.FinalRanking)
	}
	fmt.Fprintf(w, "</ul>")

	// TODO: use a template for the pagination :)
	if skip > 0 {
		fmt.Fprintf(w, `<a href="javascript:history.go(-1)">Previous page</a><span style="width: 100px">&nbsp;</span>`)
	}

	if skip != lastUsedFilename {
		urlCopy := *r.URL
		queryCopy := urlCopy.Query()
		queryCopy.Set("skip", fmt.Sprintf("%d", lastUsedFilename))
		urlCopy.RawQuery = queryCopy.Encode()
		fmt.Fprintf(w, `<a href="%s">Next page</a>`, urlCopy.RequestURI())
	}

	if len(*timingTotalPath) > 0 {
		fmt.Fprintf(tTotal, "%d\t%d\n", requestCounter, time.Now().Sub(t0).Nanoseconds()/1000/1000)
	}

	if len(*timingFirstRegexp) > 0 {
		fmt.Fprintf(tFirstRegexp, "%d\t%d\n", requestCounter, t1.Sub(t0).Nanoseconds()/1000/1000)
	}

	if len(*timingFirstIndex) > 0 {
		fmt.Fprintf(tFirstIndex, "%d\t%d\n", requestCounter, t4.Sub(t3).Nanoseconds()/1000/1000)
	}

	if len(*timingReceiveRank) > 0 {
		fmt.Fprintf(tReceiveRank, "%d\t%d\n", requestCounter, t2.Sub(t1).Nanoseconds()/1000/1000)
	}

	if len(*timingSort) > 0 {
		fmt.Fprintf(tSort, "%d\t%d\n", requestCounter, t3.Sub(t2).Nanoseconds()/1000/1000)
	}

	requestCounter++
}
