// vim:ts=4:sw=4:noexpandtab
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"sort"
	"sync"
	// This is a forked version of codesearch/regexp which returns the results
	// in a structure instead of printing to stdout/stderr directly.
	"github.com/Debian/dcs/cmd/dcs-web/varz"
	"github.com/Debian/dcs/ranking"
	"github.com/Debian/dcs/regexp"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
)

var (
	listenAddress = flag.String("listen_address", ":28082", "listen address ([host]:port)")
	unpackedPath  = flag.String("unpacked_path",
		"/dcs-ssd/unpacked/",
		"Path to the unpacked sources")
)

type SourceReply struct {
	// The number of the last used filename, needed for pagination
	LastUsedFilename int

	AllMatches []regexp.Match
}

// Serves a single file for displaying it in /show
func File(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	filename := r.Form.Get("file")

	log.Printf("requested filename *%s*\n", filename)
	// path.Join calls path.Clean so we get the shortest path without any "..".
	absPath := path.Join(*unpackedPath, filename)
	log.Printf("clean, absolute path is *%s*\n", absPath)
	if !strings.HasPrefix(absPath, *unpackedPath) {
		http.Error(w, "Path traversal is bad, mhkay?", http.StatusForbidden)
		return
	}

	file, err := os.Open(absPath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`Could not open file "%s"`, absPath), http.StatusNotFound)
		return
	}
	defer file.Close()

	io.Copy(w, file)
}

func Source(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	textQuery := r.Form.Get("q")
	limit, err := strconv.ParseInt(r.Form.Get("limit"), 10, 0)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
	filenames := r.Form["filename"]
	re, err := regexp.Compile(textQuery)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}

	log.Printf("query: text = %s, regexp = %s\n", textQuery, re)

	rankingopts := ranking.RankingOptsFromQuery(r.URL.Query())

	querystr := ranking.NewQueryStr(textQuery)

	// Create one Goroutine per filename, which means all IO will be done in
	// parallel.
	// TODO: implement a more clever way of scheduling IO. when enough results
	// are gathered, we don’t need to grep any other files, so currently we may
	// do unnecessary work.
	output := make(chan []regexp.Match)
	for _, filename := range filenames {
		go func(filename string) {
			// TODO: figure out how to safely clone a dcs/regexp
			re, err := regexp.Compile(textQuery)
			if err != nil {
				log.Printf("%s\n", err)
				return
			}

			grep := regexp.Grep{
				Regexp: re,
				Stdout: os.Stdout,
				Stderr: os.Stderr,
			}

			output <- grep.File(path.Join(*unpackedPath, filename))
		}(filename)
	}

	fmt.Printf("done, now getting the results\n")

	// TODO: also limit the number of matches per source-package, not only per file
	var reply SourceReply
	for idx, filename := range filenames {
		fmt.Printf("…in %s\n", filename)
		matches := <-output
		for idx, match := range matches {
			if limit > 0 && idx == 5 {
				// TODO: we somehow need to signal that there are more results
				// (if there are more), so that the user can expand this.
				break
			}
			fmt.Printf("match: %s\n", match)
			match.Ranking = ranking.PostRank(rankingopts, &match, &querystr)
			match.Path = match.Path[len(*unpackedPath):]
			reply.AllMatches = append(reply.AllMatches, match)
		}
		if limit > 0 && int64(len(reply.AllMatches)) >= limit {
			reply.LastUsedFilename = idx
			break
		}
	}
	jsonFiles, err := json.Marshal(&reply)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
	_, err = w.Write(jsonFiles)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}

	// Read the remaining outputs in the background.
	if reply.LastUsedFilename > 0 {
		go func(stopped, max int) {
			for i := stopped + 1; i < max; i++ {
				<-output
			}
		}(reply.LastUsedFilename, len(filenames))
	}
}

func filterByKeywords(rewritten *url.URL, files []ranking.ResultPath) []ranking.ResultPath {
	// The "package:" keyword, if specified.
	pkg := rewritten.Query().Get("package")
	// The "-package:" keywords, if specified.
	npkgs := rewritten.Query()["npackage"]
	// The "path:" keywords, if specified.
	paths := rewritten.Query()["path"]

	// Filter the filenames if the "package:" keyword was specified.
	if pkg != "" {
		fmt.Printf("Filtering for package %q\n", pkg)
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
		fmt.Printf("Excluding matches for package %q\n", npkg)
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
		fmt.Printf("Filtering for path %q\n", path)
		pathRegexp, err := regexp.Compile(path)
		if err != nil {
			return files
			// TODO: perform this validation before accepting the query, i.e. in dcs-web
			//err := common.Templates.ExecuteTemplate(w, "error.html", map[string]interface{}{
			//	"q":          r.URL.Query().Get("q"),
			//	"errormsg":   fmt.Sprintf(`%v`, err),
			//	"suggestion": template.HTML(`See <a href="http://codesearch.debian.net/faq#regexp">http://codesearch.debian.net/faq#regexp</a> for help on regular expressions.`),
			//})
			//if err != nil {
			//	http.Error(w, err.Error(), http.StatusInternalServerError)
			//}
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

	return files
}

func queryIndexBackend(query string) ([]string, error) {
	var filenames []string
	u, err := url.Parse("http://localhost:28081/index")
	if err != nil {
		return filenames, err
	}
	q := u.Query()
	q.Set("q", query)
	u.RawQuery = q.Encode()
	resp, err := http.Get(u.String())
	if err != nil {
		return filenames, err
	}

	if resp.StatusCode != 200 {
		return filenames, fmt.Errorf("Expected HTTP 200, got %q", resp.Status)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&filenames); err != nil {
		return filenames, err
	}

	return filenames, nil
}

// Reads a single JSON request from the TCP connection, performs the search and
// sends results back over the TCP connection as they appear.
func streamingQuery(conn net.Conn) {
	defer conn.Close()
	logprefix := fmt.Sprintf("[%s]", conn.RemoteAddr().String())

	type sourceRequest struct {
		Query string
		// Rewritten URL (after RewriteQuery()) with all the parameters that
		// are relevant for ranking.
		URL string
	}

	var r sourceRequest
	if err := json.NewDecoder(conn).Decode(&r); err != nil {
		log.Printf("%s Could not parse JSON request: %v\n", logprefix, err)
		return
	}

	logprefix = fmt.Sprintf("%s [%q]", logprefix, r.Query)

	// Ask the local index backend for all the filenames.
	filenames, err := queryIndexBackend(r.Query)
	if err != nil {
		log.Printf("%s Error querying index backend for query %q: %v\n", logprefix, r.Query, err)
		return
	}

	// Parse the (rewritten) URL to extract all ranking options/keywords.
	rewritten, err := url.Parse(r.URL)
	if err != nil {
		log.Fatal(err)
	}
	rankingopts := ranking.RankingOptsFromQuery(rewritten.Query())

	// Rank all the paths.
	files := make(ranking.ResultPaths, 0, len(filenames))
	for _, filename := range filenames {
		result := ranking.ResultPath{Path: filename}
		result.Rank(&rankingopts)
		if result.Ranking > -1 {
			files = append(files, result)
		}
	}

	// Filter all files that should be excluded.
	files = filterByKeywords(rewritten, files)

	// While not strictly necessary, this will lead to better results being
	// discovered (and returned!) earlier, so let’s spend a few cycles on
	// sorting the list of potential files first.
	sort.Sort(files)

	re, err := regexp.Compile(r.Query)
	if err != nil {
		log.Printf("%s Could not compile regexp: %v\n", logprefix, err)
		return
	}

	log.Printf("%s regexp = %q, %d possible files\n", logprefix, re, len(files))

	// Send the first progress update so that clients know how many files are
	// going to be searched.
	type progressUpdate struct {
		// Set to “progress”.
		Type string

		FilesProcessed int
		FilesTotal     int
	}

	encoder := json.NewEncoder(conn)

	if err := encoder.Encode(&progressUpdate{
		Type:       "progress",
		FilesTotal: len(files),
	}); err != nil {
		// TODO: relax
		log.Fatal(err)
	}

	// The tricky part here is “flow control”: if we just start grepping like
	// crazy, we will eventually run out of memory because all our writes are
	// blocked on the connection (and the goroutines need to keep the write
	// buffer in memory until the write is done).
	//
	// So instead, we start 1000 worker goroutines and feed them work through a
	// single channel. Due to these these goroutines being blocked on writing,
	// the grepping will naturally become slower.
	work := make(chan ranking.ResultPath)
	progress := make(chan int)

	go func() {
		for _, file := range files {
			work <- file
		}
		close(work)
	}()

	go func() {
		cnt := 0
		for cnt < len(files) {
			add := <-progress
			cnt += add

			// Send exactly one progress update for 10%, 20%, …
			// TODO: make the amount of updates depend on the amount of files
			// to go through. e.g. searching for “main” has 513467 files, and
			// updating only every 10% is a bit slow.
			percentage := int(float32(cnt) / float32(len(files)) * 100)
			if percentage%10 == 0 &&
				percentage < 100 &&
				int(float32(cnt-1)/float32(len(files))*100)%10 != 0 {
				if err := encoder.Encode(&progressUpdate{
					Type:           "progress",
					FilesProcessed: cnt,
					FilesTotal:     len(files),
				}); err != nil {
					// TODO: relax
					log.Fatal(err)
				}
			}
		}

		if err := encoder.Encode(&progressUpdate{
			Type:           "progress",
			FilesProcessed: len(files),
			FilesTotal:     len(files),
		}); err != nil {
			// TODO: relax
			log.Fatal(err)
		}
		close(progress)
	}()

	querystr := ranking.NewQueryStr(r.Query)

	var wg sync.WaitGroup
	wg.Add(len(files))
	numWorkers := 1000
	if len(files) < 1000 {
		numWorkers = len(files)
	}
	for i := 0; i < numWorkers; i++ {
		go func() {
			re, err := regexp.Compile(r.Query)
			if err != nil {
				log.Printf("%s\n", err)
				return
			}

			grep := regexp.Grep{
				Regexp: re,
				Stdout: os.Stdout,
				Stderr: os.Stderr,
			}

			for file := range work {
				sourcePkgName := file.Path[file.SourcePkgIdx[0]:file.SourcePkgIdx[1]]
				if rankingopts.Pathmatch {
					file.Ranking += querystr.Match(&file.Path)
				}
				if rankingopts.Sourcepkgmatch {
					file.Ranking += querystr.Match(&sourcePkgName)
				}
				if rankingopts.Weighted {
					file.Ranking += 0.1460 * querystr.Match(&file.Path)
					file.Ranking += 0.0008 * querystr.Match(&sourcePkgName)
				}

				// TODO: figure out how to safely clone a dcs/regexp
				matches := grep.File(path.Join(*unpackedPath, file.Path))
				for _, match := range matches {
					match.Ranking = ranking.PostRank(rankingopts, &match, &querystr)
					match.PathRank = file.Ranking
					match.Path = match.Path[len(*unpackedPath):]
					// NB: populating match.Ranking happens in
					// cmd/dcs-web/querymanager because it depends on at least
					// one other result.

					if err := encoder.Encode(&match); err != nil {
						// TODO: relax
						log.Fatal(err)
					}
				}

				progress <- 1

				wg.Done()
			}
		}()
	}

	wg.Wait()

	log.Printf("%s Sent all results.\n", logprefix)
}

func main() {
	flag.Parse()
	fmt.Println("Debian Code Search source-backend")

	listener, err := net.Listen("tcp", ":26082")
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Fatalf("Error accepting session: %v", err)
			}

			go streamingQuery(conn)
		}
	}()

	http.HandleFunc("/source", Source)
	http.HandleFunc("/file", File)
	http.HandleFunc("/varz", varz.Varz)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
