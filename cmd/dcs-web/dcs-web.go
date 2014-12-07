// vim:ts=4:sw=4:noexpandtab
package main

import (
	"code.google.com/p/go.net/websocket"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Debian/dcs/cmd/dcs-web/common"
	"github.com/Debian/dcs/cmd/dcs-web/health"
	"github.com/Debian/dcs/cmd/dcs-web/show"
	"github.com/Debian/dcs/goroutinez"
	"github.com/Debian/dcs/varz"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"
)

var (
	listenAddress = flag.String("listen_address",
		":28080",
		"listen address ([host]:port)")
	memprofile = flag.String("memprofile", "", "Write memory profile to this file")
	staticPath = flag.String("static_path",
		"./static/",
		"Path to static assets such as *.css")
	accessLogPath = flag.String("access_log_path",
		"",
		"Where to write access.log entries (in Apache Common Log Format). Disabled if empty.")

	accessLog *os.File

	resultsPathRe  = regexp.MustCompile(`^/results/([^/]+)/(perpackage_` + strconv.Itoa(resultsPerPackage) + `_)?page_([0-9]+).json$`)
	packagesPathRe = regexp.MustCompile(`^/results/([^/]+)/packages.json$`)
)

func InstantServer(ws *websocket.Conn) {
	// The additional ":" at the end is necessary so that we don’t need to
	// distinguish between the two cases (X-Forwarded-For, without a port, and
	// RemoteAddr, with a part) in the code below.
	src := ws.Request().Header.Get("X-Forwarded-For") + ":"
	remoteaddr := ws.Request().RemoteAddr
	if src == "" || (!strings.HasPrefix(remoteaddr, "[::1]:") &&
		!strings.HasPrefix(remoteaddr, "127.0.0.1:")) {
		src = remoteaddr
	}
	log.Printf("Accepted websocket connection from %q\n", src)

	type Query struct {
		Query string
	}
	var q Query
	for {
		err := json.NewDecoder(ws).Decode(&q)
		if err != nil {
			log.Printf("[%s] error reading query: %v\n", src, err)
			return
		}
		log.Printf("[%s] Received query %v\n", src, q)
		if strings.TrimSpace(q.Query) == "" || strings.TrimSpace(q.Query) == "?q=" {
			log.Printf("[%s] Refusing empty query\n", src)
			return
		}

		// Uniquely (well, good enough) identify this query for a couple of minutes
		// (as long as we want to cache results). We could try to normalize the
		// query before hashing it, but that seems hardly worth the complexity.
		h := fnv.New64()
		io.WriteString(h, q.Query)
		identifier := fmt.Sprintf("%x", h.Sum64())

		cached := maybeStartQuery(identifier, src, q.Query)

		// Create an apache common log format entry.
		if accessLog != nil {
			responseCode := 200
			if cached {
				responseCode = 304
			}
			remoteIP := src
			if idx := strings.LastIndex(remoteIP, ":"); idx > -1 {
				remoteIP = remoteIP[:idx]
			}
			fmt.Fprintf(accessLog, "%s - - [%s] \"GET /instantws?%s HTTP/1.1\" %d -\n",
				remoteIP, time.Now().Format("02/Jan/2006:15:04:05 -0700"), q.Query, responseCode)
		}

		lastseen := -1
		for {
			message, sequence := getEvent(identifier, lastseen)
			lastseen = sequence
			// This message was obsoleted by a more recent one, e.g. a more
			// recent progress update obsoletes all earlier progress updates.
			if *message.obsolete {
				continue
			}
			if len(message.data) == 0 {
				// TODO: tell the client that a new query can be sent
				break
			}
			written, err := ws.Write(message.data)
			if err != nil {
				log.Printf("[%s] Error writing to websocket, closing: %v\n", src, err)
				return
			}
			if written != len(message.data) {
				log.Printf("[%s] Could only write %d of %d bytes to websocket, closing.\n", src, written, len(message.data))
				return
			}
		}
		log.Printf("[%s] query done. waiting for a new one\n", src)
	}
}

func ResultsHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: ideally, this would also start the search in the background to avoid waiting for the round-trip to the client.

	// TODO: also, what about non-javascript clients?

	// Try to match /page_n.json or /perpackage_2_page_n.json
	matches := resultsPathRe.FindStringSubmatch(r.URL.Path)
	log.Printf("matches for %q = %v\n", r.URL.Path, matches)
	if matches == nil || len(matches) != 4 {
		// See whether it’s /packages.json, then.
		matches = packagesPathRe.FindStringSubmatch(r.URL.Path)
		if matches == nil || len(matches) != 2 {
			// While this just serves index.html, the javascript part of index.html
			// realizes the path starts with /results/ and starts the search, then
			// requests the specified page on search completion.
			http.ServeFile(w, r, filepath.Join(*staticPath, "index.html"))
			return
		}

		queryid := matches[1]
		_, ok := state[queryid]
		if !ok {
			http.Error(w, "No such query.", http.StatusNotFound)
			return
		}

		startJsonResponse(w)

		packages := state[queryid].allPackagesSorted

		if err := json.NewEncoder(w).Encode(struct{ Packages []string }{packages}); err != nil {
			http.Error(w, fmt.Sprintf("Could not encode packages: %v", err), http.StatusInternalServerError)
		}
		return
	}

	queryid := matches[1]
	page, err := strconv.Atoi(matches[3])
	if err != nil {
		log.Fatal("Could not convert %q into a number: %v\n", matches[2], err)
	}
	perpackage := (matches[2] == "perpackage_2_")
	_, ok := state[queryid]
	if !ok {
		http.Error(w, "No such query.", http.StatusNotFound)
		return
	}

	if !perpackage {
		err = writeResults(queryid, page, w, w, r)
	} else {
		err = writePerPkgResults(queryid, page, w, w, r)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func main() {
	flag.Parse()
	common.LoadTemplates()
	if *accessLogPath != "" {
		var err error
		accessLog, err = os.OpenFile(*accessLogPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			log.Fatal(err)
		}
	}

	varz.Set("failed-queries", 0)

	fmt.Println("Debian Code Search webapp")

	health.StartChecking()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Check if a static file was requested with full name
		name := filepath.Join(*staticPath, r.URL.Path)
		if _, err := os.Stat(name); err == nil {
			http.ServeFile(w, r, name)
			return
		}

		// Or maybe /faq, which resolves to /faq.html
		name = name + ".html"
		if _, err := os.Stat(name); err == nil {
			http.ServeFile(w, r, name)
			return
		}

		http.ServeFile(w, r, filepath.Join(*staticPath, "index.html"))
	})
	http.HandleFunc("/favicon.ico", http.NotFound)
	http.HandleFunc("/varz", varz.Varz)
	http.HandleFunc("/goroutinez", goroutinez.Goroutinez)
	http.HandleFunc("/search", Search)
	http.HandleFunc("/show", show.Show)
	http.HandleFunc("/memprof", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("writing memprof")
		if *memprofile != "" {
			f, err := os.Create(*memprofile)
			if err != nil {
				log.Fatal(err)
			}
			pprof.WriteHeapProfile(f)
			f.Close()
			return
		}
	})

	http.HandleFunc("/results/", ResultsHandler)
	http.HandleFunc("/perpackage-results/", PerPackageResultsHandler)
	http.HandleFunc("/queryz", QueryzHandler)

	http.Handle("/instantws", websocket.Handler(InstantServer))

	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
