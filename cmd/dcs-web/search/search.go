// vim:ts=4:sw=4:noexpandtab
package search

import (
	"bytes"
	"code.google.com/p/codesearch/regexp"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Debian/dcs/cmd/dcs-web/common"
	"github.com/Debian/dcs/ranking"
	"html/template"
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
	Ctxp2   string
	Ctxp1   string
	Context string
	Ctxn1   string
	Ctxn2   string
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
		if result.Ranking > -1 {
			indexResults <- result
		}
	}
	done <- 1
}

// TODO: refactor this with sendIndexQuery.
func sendSourceQuery(query url.URL, values chan ranking.ResultPaths, cont chan bool, matches chan Match, done chan int, allResults bool, skip int) {

	defer func() {
		cont <- false
	}()

	// How many results per page to display tops.
	limit := 0
	if !allResults {
		limit = 40
	}

	query.Scheme = "http"
	query.Host = *common.SourceBackends
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
			done <- lastUsedFilename
			return
		}
		start := skip
		if start < 0 {
			start = 0
		}
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
				lastUsedFilename = skipped + start + reply.LastUsedFilename
				fmt.Printf("last used filename: %d -> %d\n", reply.LastUsedFilename, lastUsedFilename)
			}

			for _, match := range reply.AllMatches {
				matches <- match
				numMatches++
			}

			if numMatches >= limit && limit > 0 {
				fmt.Printf("numMatches = %d, limit = %d, end\n", numMatches, limit)
				done <- lastUsedFilename
				return
			}

			// If there were no matches, we provide more filenames and retry
			start += 500
		}
		if skip > 0 {
			skip -= len(filenames)
		}
		skipped += len(filenames)
		log.Printf("skip = %d, skipped = %d, len(filenames) = %d, start = %d\n",
			skip, skipped, len(filenames), start)
		cont <- true
	}

	done <- lastUsedFilename
}

func Search(w http.ResponseWriter, r *http.Request) {
	var tinit, t0, t1, t2, t3, t4 time.Time

	tinit = time.Now()

	// Rewrite the query to extract words like "lang:c" from the querystring
	// and place them in parameters.
	rewritten := RewriteQuery(*r.URL)

	query := rewritten.Query()
	// The "package:" keyword, if specified.
	pkg := rewritten.Query().Get("package")
	// The "-package:" keyword, if specified.
	npkgs := rewritten.Query()["npackage"]
	// The "path:" keyword, if specified.
	paths := rewritten.Query()["path"]

	// Usage of this flag should be restricted to local IP addresses or
	// something like that (it causes a lot of load, but it makes analyzing the
	// search engine’s ranking easier).
	allResults := query.Get("all") == "1"

	// Users can configurable which ranking factors (with what weight) they
	// want to use. rankingopts stores these values, extracted from the query
	// parameters.
	rankingopts := ranking.RankingOptsFromQuery(query)

	querystr := ranking.NewQueryStr(query.Get("q"))

	if len(query.Get("q")) < 3 {
		err := common.Templates.ExecuteTemplate(w, "error.html", map[string]interface{}{
			"q":        r.URL.Query().Get("q"),
			"errormsg": "Your search term is too short. You need at least 3 characters.",
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		return
	}

	_, err := regexp.Compile(query.Get("q"))
	if err != nil {
		err := common.Templates.ExecuteTemplate(w, "error.html", map[string]interface{}{
			"q":          r.URL.Query().Get("q"),
			"errormsg":   fmt.Sprintf(`%v`, err),
			"suggestion": template.HTML(`See <a href="http://codesearch.debian.net/faq#regexp">http://codesearch.debian.net/faq#regexp</a> for help on regular expressions.`),
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		return
	}

	// Number of files to skip when searching. Used for pagination.
	skip64, _ := strconv.ParseInt(query.Get("skip"), 10, 0)
	skip := int(skip64)

	log.Printf("Search query: term %q, URL %q", query.Get("q"), rewritten.String())
	fmt.Printf("opts: %v\n", rankingopts)

	fmt.Printf("Query parsed after %v\n", time.Now().Sub(tinit))

	// TODO: compile the regular expression right here so that we don’t do it N
	// times and can properly error out.

	// Send the query to all index backends (our index is sharded into multiple
	// pieces).
	backends := strings.Split(*indexBackends, ",")
	done := make(chan int)
	indexResults := make(chan ranking.ResultPath, 10)
	t0 = time.Now()
	for _, backend := range backends {
		fmt.Printf("Sending query to " + backend)
		go sendIndexQuery(rewritten, backend, indexResults, done, rankingopts)
	}

	// Close the result channel when all index queries are done so that we can
	// use range on the result channel.
	go func() {
		for i := 0; i < len(backends); i++ {
			<-done
		}
		close(indexResults)
	}()

	var files ranking.ResultPaths
	// We also keep the files in a map with their path as the key so that we
	// can correlate a match to a (ranked!) filename later on.
	fileMap := make(map[string]ranking.ResultPath)
	for result := range indexResults {
		// Time to the first result (≈ time to query the regexp index in
		// case len(backends) == 1)
		if t1.IsZero() {
			t1 = time.Now()
		}
		files = append(files, result)
	}

	// Time to receive and rank the results
	t2 = time.Now()
	log.Printf("All %d index backend results after %v\n", len(files), t2.Sub(t0))

	// Filter the filenames if the "package:" keyword was specified.
	if pkg != "" {
		fmt.Printf(`Filtering for package "%s"\n`, pkg)
		filtered := make(ranking.ResultPaths, 0, len(files))
		for _, file := range files {
			// XXX: Do we want this to be a regular expression match, too?
			if file.Path[file.SourcePkgIdx[0]:file.SourcePkgIdx[1]] != pkg {
				continue
			}

			filtered = append(filtered, file)
		}

		files = filtered
	}
	// Filter the filenames if the "-package:" keyword was specified.
	for _, npkg := range npkgs {
		fmt.Printf(`Excluding matches for package "%s"\n`, npkg)
		filtered := make(ranking.ResultPaths, 0, len(files))
		for _, file := range files {
			// XXX: Do we want this to be a regular expression match, too?
			if file.Path[file.SourcePkgIdx[0]:file.SourcePkgIdx[1]] == npkg {
				continue
			}

			filtered = append(filtered, file)
		}

		files = filtered
	}

	for _, path := range paths {
		fmt.Printf(`Filtering for path "%s"\n`, path)
		pathRegexp, err := regexp.Compile(path)
		if err != nil {
			err := common.Templates.ExecuteTemplate(w, "error.html", map[string]interface{}{
				"q":          r.URL.Query().Get("q"),
				"errormsg":   fmt.Sprintf(`%v`, err),
				"suggestion": template.HTML(`See <a href="http://codesearch.debian.net/faq#regexp">http://codesearch.debian.net/faq#regexp</a> for help on regular expressions.`),
			})
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}

			return
		}

		filtered := make(ranking.ResultPaths, 0, len(files))
		for _, file := range files {
			if pathRegexp.MatchString(file.Path, true, true) == -1 {
				continue
			}

			filtered = append(filtered, file)
		}

		files = filtered
	}

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
			fmt.Printf("ranking 1000 starting from %d\n", start)
			batch := ranking.ResultPaths(resultWindow(files, start, 1000))

			for idx, result := range batch {
				sourcePkgName := result.Path[result.SourcePkgIdx[0]:result.SourcePkgIdx[1]]
				if rankingopts.Pathmatch {
					batch[idx].Ranking += querystr.Match(&result.Path)
				}
				if rankingopts.Sourcepkgmatch {
					batch[idx].Ranking += querystr.Match(&sourcePkgName)
				}
				if rankingopts.Weighted {
					batch[idx].Ranking += 0.1460 * querystr.Match(&result.Path)
					batch[idx].Ranking += 0.0008 * querystr.Match(&sourcePkgName)
				}
				fileMap[result.Path] = batch[idx]
			}

			sort.Sort(batch)
			values <- batch
			if !<-cont {
				fmt.Printf("ranking goroutine exits\n")
				return
			}

			start += 1000
		}

		// Close the channel to signal that there are no more values available
		close(values)

		// Read value from cont goroutine to avoid a blocking write in
		// sendSourceQuery (effectively leading to goroutine leaks).
		<-cont
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

	// People seem to be distracted by large negative numbers, so we rather
	// show a 0 in case there were no source results :-).
	if t4.IsZero() {
		t4 = t3
	}

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

	// Show a helpful message when there are no search results instead of just
	// an empty list.
	if len(results) == 0 {
		err := common.Templates.ExecuteTemplate(w, "error.html", map[string]interface{}{
			"q":          r.URL.Query().Get("q"),
			"errormsg":   "No search results!",
			"suggestion": template.HTML(`Debian Code Search is case-sensitive. Also, search queries are interpreted as <a href="http://codesearch.debian.net/faq#regexp">regular expressions</a>.`),
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		return

	}

	// NB: We send the template output to a buffer because that is faster. We
	// also just use the template for the header of the page and then print the
	// results directly from Go, which saves ≈ 10 ms (!).
	outputBuffer := new(bytes.Buffer)
	err = common.Templates.ExecuteTemplate(outputBuffer, "results.html", map[string]interface{}{
		//"results": results,
		"t0":         t1.Sub(t0),
		"t1":         t2.Sub(t1),
		"t2":         t3.Sub(t2),
		"t3":         t4.Sub(t3),
		"numfiles":   len(files),
		"numresults": len(results),
		"timing":     (rewritten.Query().Get("notiming") != "1"),
		"q":          r.URL.Query().Get("q"),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	outputBuffer.WriteTo(w)

	context := make([]string, 5)
	for _, result := range results {
		ctx := context[:0]
		if val := strings.TrimSpace(result.Ctxp2); val != "" {
			ctx = append(ctx, result.Ctxp2)
		}
		if val := strings.TrimSpace(result.Ctxp1); val != "" {
			ctx = append(ctx, result.Ctxp1)
		}
		ctx = append(ctx, "<strong>"+result.Context+"</strong>")
		if val := strings.TrimSpace(result.Ctxn1); val != "" {
			ctx = append(ctx, result.Ctxn1)
		}
		if val := strings.TrimSpace(result.Ctxn2); val != "" {
			ctx = append(ctx, result.Ctxn2)
		}
		fmt.Fprintf(w, `<li><a href="/show?file=%s&amp;line=%d&amp;numfiles=%d#L%d"><code><strong>%s</strong>%s:%d</code></a><br><pre>%s</pre>
<small>PathRank: %g, Rank: %g, Final: %g</small></li>`+"\n",
			url.QueryEscape(result.SourcePackage+result.RelativePath),
			result.Line,
			len(files),
			result.Line,
			result.SourcePackage,
			result.RelativePath,
			result.Line,
			strings.Replace(strings.Join(ctx, "<br>"), "\t", "    ", -1),
			result.PathRanking,
			result.Ranking,
			result.FinalRanking)
	}
	fmt.Fprintf(w, "</ul>")

	fmt.Fprintf(w, `<div id="pagination">`)
	if skip > 0 {
		urlCopy := *r.URL
		queryCopy := urlCopy.Query()
		// Pop one value from nextPrev
		prev := strings.Split(queryCopy.Get("prev"), ".")
		// We always have one element, but let’s make sure, it’s user-input
		// after all.
		if len(prev) > 0 {
			queryCopy.Set("skip", prev[len(prev)-1])
			queryCopy.Set("prev", strings.Join(prev[:len(prev)-1], "."))
			urlCopy.RawQuery = queryCopy.Encode()
			fmt.Fprintf(w, `<a href="%s">Previous page</a><div style="display: inline-block; width: 100px">&nbsp;</div>`, urlCopy.RequestURI())
		}
	}

	if skip != lastUsedFilename {
		urlCopy := *r.URL
		queryCopy := urlCopy.Query()
		queryCopy.Set("skip", fmt.Sprintf("%d", lastUsedFilename))
		nextPrev := queryCopy.Get("prev")
		if nextPrev == "" {
			nextPrev = "0"
		} else {
			// We use dot as a separator because it doesn’t get url-encoded
			// (see RFC 3986 section 2.3).
			nextPrev = fmt.Sprintf("%s.%d", nextPrev, skip)
		}
		queryCopy.Set("prev", nextPrev)
		urlCopy.RawQuery = queryCopy.Encode()
		fmt.Fprintf(w, `<a href="%s">Next page</a>`, urlCopy.RequestURI())
	}

	err = common.Templates.ExecuteTemplate(w, "footer.html", map[string]interface{}{
		"version": common.Version,
	})
	if err != nil {
		log.Printf("template error: %v\n", err.Error())
		// We cannot use http.Error since it sends headers and we already did that.
		//http.Error(w, err.Error(), http.StatusInternalServerError)
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
