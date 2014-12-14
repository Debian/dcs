// vim:ts=4:sw=4:noexpandtab
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Debian/dcs/proto"
	"github.com/Debian/dcs/ranking"
	"github.com/Debian/dcs/regexp"
	"github.com/Debian/dcs/varz"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	capn "github.com/glycerine/go-capnproto"
)

var (
	listenAddress          = flag.String("listen_address", ":28082", "listen address ([host]:port)")
	listenAddressStreaming = flag.String("listen_address_streaming", ":26082", "listen address for streaming queries using capnproto ([host]:port)")
	unpackedPath           = flag.String("unpacked_path",
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

func filterByKeywords(rewritten *url.URL, files []ranking.ResultPath) []ranking.ResultPath {
	// The "package:" keyword, if specified.
	pkg := rewritten.Query().Get("package")
	// The "-package:" keywords, if specified.
	npkgs := rewritten.Query()["npackage"]
	// The "path:" keywords, if specified.
	paths := rewritten.Query()["path"]
	// The "-path" keywords, if specified.
	npaths := rewritten.Query()["npath"]

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

	for _, path := range npaths {
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
			if pathRegexp.MatchString(file.Path, true, true) != -1 {
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

func sendProgressUpdate(conn net.Conn, connMu *sync.Mutex, filesProcessed, filesTotal int) (int64, error) {
	seg := capn.NewBuffer(nil)
	z := proto.NewRootZ(seg)
	p := proto.NewProgressUpdate(seg)
	p.SetFilesprocessed(uint64(filesProcessed))
	p.SetFilestotal(uint64(filesTotal))
	z.SetProgressupdate(p)
	connMu.Lock()
	defer connMu.Unlock()
	return seg.WriteToPacked(conn)
}

// Reads a single JSON request from the TCP connection, performs the search and
// sends results back over the TCP connection as they appear.
func streamingQuery(conn net.Conn) {
	defer conn.Close()
	connMu := new(sync.Mutex)
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
	if _, err := sendProgressUpdate(conn, connMu, 0, len(files)); err != nil {
		log.Printf("%s %v\n", logprefix, err)
		return
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

	var wg sync.WaitGroup
	// We add the additional 1 for the progress updater goroutine. It also
	// needs to be done before we can return, otherwise it will try to use the
	// (already closed) network connection, which is a fatal error.
	wg.Add(len(files) + 1)

	go func() {
		for _, file := range files {
			work <- file
		}
		close(work)
	}()

	go func() {
		cnt := 0
		errorShown := false
		var lastProgressUpdate time.Time
		progressInterval := 2*time.Second + time.Duration(rand.Int63n(int64(500*time.Millisecond)))
		for cnt < len(files) {
			add := <-progress
			cnt += add

			if time.Since(lastProgressUpdate) > progressInterval {
				if _, err := sendProgressUpdate(conn, connMu, cnt, len(files)); err != nil {
					if !errorShown {
						log.Printf("%s %v\n", logprefix, err)
						// We need to read the 'progress' channel, so we cannot
						// just exit the loop here. Instead, we suppress all
						// error messages after the first one.
						errorShown = true
					}
				}
				lastProgressUpdate = time.Now()
			}
		}

		if _, err := sendProgressUpdate(conn, connMu, len(files), len(files)); err != nil {
			log.Printf("%s %v\n", logprefix, err)
		}
		close(progress)

		wg.Done()
	}()

	querystr := ranking.NewQueryStr(r.Query)

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
					//match.Path = match.Path[len(*unpackedPath):]
					// NB: populating match.Ranking happens in
					// cmd/dcs-web/querymanager because it depends on at least
					// one other result.

					// TODO: ideally, we’d get capn buffers from grep.File(), let’s do that after profiling the decoding performance
					seg := capn.NewBuffer(nil)
					z := proto.NewRootZ(seg)
					m := proto.NewMatch(seg)
					m.SetPath(match.Path[len(*unpackedPath):])
					m.SetLine(uint32(match.Line))
					m.SetPackage(m.Path()[:strings.Index(m.Path(), "/")])
					m.SetCtxp2(match.Ctxp2)
					m.SetCtxp1(match.Ctxp1)
					m.SetContext(match.Context)
					m.SetCtxn1(match.Ctxn1)
					m.SetCtxn2(match.Ctxn2)
					m.SetPathrank(match.PathRank)
					m.SetRanking(match.Ranking)
					z.SetMatch(m)

					connMu.Lock()
					if _, err := seg.WriteToPacked(conn); err != nil {
						connMu.Unlock()
						log.Printf("%s %v\n", logprefix, err)
						// Drain the work channel, but without doing any work.
						// This effectively exits the worker goroutine(s)
						// cleanly.
						for _ = range work {
						}
						break
					}
					connMu.Unlock()
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
	rand.Seed(time.Now().UnixNano())
	fmt.Println("Debian Code Search source-backend")

	listener, err := net.Listen("tcp", *listenAddressStreaming)
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

	http.HandleFunc("/file", File)
	http.HandleFunc("/varz", varz.Varz)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
