// vim:ts=4:sw=4:noexpandtab
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"

	"github.com/Debian/dcs/cmd/dcs-web/common"
	"github.com/Debian/dcs/cmd/dcs-web/health"
	"github.com/Debian/dcs/cmd/dcs-web/search"
	"github.com/Debian/dcs/cmd/dcs-web/show"
	"github.com/Debian/dcs/goroutinez"
	"github.com/Debian/dcs/grpcutil"
	"github.com/Debian/dcs/internal/index"
	"github.com/Debian/dcs/internal/proto/dcspb"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
	dcsregexp "github.com/Debian/dcs/regexp"
	_ "github.com/Debian/dcs/varz"
	"github.com/opentracing-contrib/go-stdlib/nethttp"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/uber/jaeger-client-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
	_ "golang.org/x/net/trace"
	"golang.org/x/net/websocket"
)

var (
	listenAddressPlain = flag.String("listen_address_http",
		"",
		"listen address ([host]:port)")
	listenAddress = flag.String("listen_address",
		"",
		"listen address ([host]:port) for gRPC/TLS")
	memprofile = flag.String("memprofile", "", "Write memory profile to this file")
	staticPath = flag.String("static_path",
		"./static/",
		"Path to static assets such as *.css")
	accessLogPath = flag.String("access_log_path",
		"",
		"Where to write access.log entries (in Apache Common Log Format). Disabled if empty.")
	tlsCertPath = flag.String("tls_cert_path", "", "Path to a .pem file containing the TLS certificate.")
	tlsKeyPath  = flag.String("tls_key_path", "", "Path to a .pem file containing the TLS private key.")
	jaegerAgent = flag.String("jaeger_agent",
		"localhost:5775",
		"host:port of a github.com/uber/jaeger agent")

	accessLog *os.File

	resultsPathRe  = regexp.MustCompile(`^/results/([^/]+)/(perpackage_` + strconv.Itoa(resultsPerPackage) + `_)?page_([0-9]+).json$`)
	packagesPathRe = regexp.MustCompile(`^/results/([^/]+)/packages.(json|txt)$`)
	redirectPathRe = regexp.MustCompile(`^/(?:perpackage-)?results/([^/]+)(?:/[0-9]+)?/page_([0-9]+)`)

	activeQueries = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "queries_active",
			Help: "Number of active queries (i.e. not all results are in yet).",
		})

	failedQueries = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "queries_failed",
			Help: "Number of failed queries.",
		})
)

func init() {
	prometheus.MustRegister(activeQueries)
	prometheus.MustRegister(failedQueries)
}

func validateQuery(query string) error {
	// Parse the query and see whether the resulting trigram query is
	// non-empty. This is to catch queries like “package:debian”.
	fakeUrl, err := url.Parse(query)
	if err != nil {
		return err
	}
	rewritten := search.RewriteQuery(*fakeUrl)
	log.Printf("rewritten query = %q\n", rewritten.String())
	re, err := dcsregexp.Compile(rewritten.Query().Get("q"))
	if err != nil {
		return err
	}
	indexQuery := index.RegexpQuery(re.Syntax)
	log.Printf("trigram = %v, sub = %v", indexQuery.Trigram, indexQuery.Sub)
	if len(indexQuery.Trigram) == 0 && len(indexQuery.Sub) == 0 {
		return fmt.Errorf("Empty index query")
	}
	return nil
}

func EventsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.FormValue("q")
	if query == "" {
		query = strings.TrimPrefix(r.URL.Path, "/events/")
	}
	span := opentracing.SpanFromContext(ctx)
	span.SetOperationName("Events: " + query)
	w.Header().Set("Content-Type", "text/event-stream")

	// The additional ":" at the end is necessary so that we don’t need to
	// distinguish between the two cases (X-Forwarded-For, without a port, and
	// RemoteAddr, with a part) in the code below.
	src := r.Header.Get("X-Forwarded-For") + ":"
	if src == ":" || (!strings.HasPrefix(r.RemoteAddr, "[::1]:") &&
		!strings.HasPrefix(r.RemoteAddr, "127.0.0.1:")) {
		src = r.RemoteAddr
	}
	q := "q=" + url.QueryEscape(query)

	log.Printf("[%s] (events) Received query %q\n", src, q)
	if err := validateQuery("?" + q); err != nil {
		log.Printf("[%s] Query %q failed validation: %v\n", src, q, err)
		b, _ := json.Marshal(struct {
			Type         string
			ErrorType    string
			ErrorMessage string
		}{
			Type:         "error",
			ErrorType:    "invalidquery",
			ErrorMessage: err.Error(),
		})
		if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", 0, string(b)); err != nil {
			log.Printf("[%s] aborting, could not write: %v\n", src, err)
			return
		}
		return
	}

	// Uniquely (well, good enough) identify this query for a couple of minutes
	// (as long as we want to cache results). We could try to normalize the
	// query before hashing it, but that seems hardly worth the complexity.
	h := fnv.New64()
	io.WriteString(h, q)
	identifier := fmt.Sprintf("%x", h.Sum64())

	cached, err := maybeStartQuery(ctx, identifier, src, q)
	if err != nil {
		log.Printf("[%s] could not start query: %v\n", src, err)
		http.Error(w, "Could not start query", http.StatusInternalServerError)
		return
	}

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
		fmt.Fprintf(accessLog, "%s - - [%s] \"GET /events/%s HTTP/1.1\" %d -\n",
			remoteIP, time.Now().Format("02/Jan/2006:15:04:05 -0700"), q, responseCode)
	}

	// TODO: use Last-Event-ID header
	lastseen := -1
	sent := 0
	for {
		message, sequence := getEvent(identifier, lastseen)
		lastseen = sequence
		// This message was obsoleted by a more recent one, e.g. a more
		// recent progress update obsoletes all earlier progress updates.
		if *message.obsolete {
			continue
		}
		if len(message.data) == 0 {
			break
		}
		if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", sequence, message.data); err != nil {
			log.Printf("[%s] aborting, could not write: %v\n", src, err)
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		sent++
	}

	if sent == 0 {
		w.WriteHeader(http.StatusNoContent)
		fmt.Fprintln(w, "No content")
	}
}

func InstantServer(ws *websocket.Conn) {
	ctx := ws.Request().Context()
	// The additional ":" at the end is necessary so that we don’t need to
	// distinguish between the two cases (X-Forwarded-For, without a port, and
	// RemoteAddr, with a part) in the code below.
	src := ws.Request().Header.Get("X-Forwarded-For") + ":"
	remoteaddr := ws.Request().RemoteAddr
	if src == ":" || (!strings.HasPrefix(remoteaddr, "[::1]:") &&
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

		// span := opentracing.SpanFromContext(ctx)
		// span.SetOperationName("Websocket: " + q.Query)

		if err := validateQuery("?" + q.Query); err != nil {
			log.Printf("[%s] Query %q failed validation: %v\n", src, q.Query, err)
			ws.Write([]byte(`{"Type":"error", "ErrorType":"invalidquery"}`))
			continue
		}

		// Uniquely (well, good enough) identify this query for a couple of minutes
		// (as long as we want to cache results). We could try to normalize the
		// query before hashing it, but that seems hardly worth the complexity.
		h := fnv.New64()
		io.WriteString(h, q.Query)
		identifier := fmt.Sprintf("%x", h.Sum64())

		cached, err := maybeStartQuery(ctx, identifier, src, q.Query)
		if err != nil {
			log.Printf("[%s] could not start query: %v\n", src, err)
			ws.Write([]byte(`{"Type":"error", "ErrorType":"failed"}`))
			continue
		}

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

	// Try to match /page_n.json or /perpackage_2_page_n.json
	matches := resultsPathRe.FindStringSubmatch(r.URL.Path)
	log.Printf("matches for %q = %v\n", r.URL.Path, matches)
	if matches == nil || len(matches) != 4 {
		// See whether it’s /packages.json, then.
		matches = packagesPathRe.FindStringSubmatch(r.URL.Path)
		if matches == nil || len(matches) != 3 {
			matches = redirectPathRe.FindStringSubmatch(r.URL.Path)
			if len(matches) < 3 {
				http.Error(w, "Bad request", http.StatusBadRequest)
				return
			}
			pageSuffix := "&page=" + matches[2]
			if matches[2] == "0" {
				pageSuffix = ""
			}
			http.Redirect(w, r, "/search?q="+matches[1]+pageSuffix, http.StatusFound)
			return
		}

		queryid := matches[1]
		_, ok := state[queryid]
		if !ok {
			http.Error(w, "No such query.", http.StatusNotFound)
			return
		}

		if matches[2] == "json" {
			startJsonResponse(w)
		}

		packages := state[queryid].allPackagesSorted

		switch matches[2] {
		case "json":
			if err := json.NewEncoder(w).Encode(struct{ Packages []string }{packages}); err != nil {
				http.Error(w, fmt.Sprintf("Could not encode packages: %v", err), http.StatusInternalServerError)
			}
		case "txt":
			if _, err := w.Write([]byte(strings.Join(packages, "\n") + "\n")); err != nil {
				http.Error(w, fmt.Sprintf("Could not write packages: %v", err), http.StatusInternalServerError)
			}
		}
		return
	}

	queryid := matches[1]
	page, err := strconv.Atoi(matches[3])
	if err != nil {
		log.Fatalf("Could not convert %q into a number: %v\n", matches[3], err)
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

type server struct{}

func (s *server) Search(req *dcspb.SearchRequest, stream dcspb.DCS_SearchServer) error {
	ctx := stream.Context()
	query := req.GetQuery()
	span := opentracing.SpanFromContext(ctx)
	span.SetOperationName("gRPC: Search: " + query)

	src := "gRPC" // TODO: get remote address
	q := "q=" + url.QueryEscape(query)

	log.Printf("[%s] (events) Received query %q\n", src, q)
	if err := validateQuery("?" + q); err != nil {
		log.Printf("[%s] Query %q failed validation: %v\n", src, q, err)
		return fmt.Errorf("invalid query: %v", err)
	}

	// Uniquely (well, good enough) identify this query for a couple of minutes
	// (as long as we want to cache results). We could try to normalize the
	// query before hashing it, but that seems hardly worth the complexity.
	h := fnv.New64()
	io.WriteString(h, q)
	identifier := fmt.Sprintf("%x", h.Sum64())

	cached, err := maybeStartQuery(ctx, identifier, src, q)
	if err != nil {
		return fmt.Errorf("query(%s): %v", query, err)
	}

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
		fmt.Fprintf(accessLog, "%s - - [%s] \"GET /events/%s HTTP/1.1\" %d -\n",
			remoteIP, time.Now().Format("02/Jan/2006:15:04:05 -0700"), q, responseCode)
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
			break
		}
		ev, err := toEventProto(message.data)
		if err != nil {
			return err
		}
		if err := stream.Send(ev); err != nil {
			return err
		}
	}

	return nil
}

// TODO: consider refactoring so that protos are retained instead of JSON
// bytes. maybe accompanied by a lazily-initialized JSON version?
func toEventProto(data []byte) (*dcspb.Event, error) {
	var messageType struct {
		Type string
	}
	if err := json.Unmarshal(data, &messageType); err != nil {
		return nil, err
	}
	switch messageType.Type {
	case "progress":
		var p struct {
			QueryId        string
			FilesProcessed int
			FilesTotal     int
			Results        int
		}
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		return &dcspb.Event{
			Data: &dcspb.Event_Progress{
				Progress: &dcspb.Progress{
					QueryId:        p.QueryId,
					FilesProcessed: int64(p.FilesProcessed),
					FilesTotal:     int64(p.FilesTotal),
					Results:        int64(p.Results),
				},
			},
		}, nil

	case "pagination":
		var p struct {
			QueryId     string
			ResultPages int
		}
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		return &dcspb.Event{
			Data: &dcspb.Event_Pagination{
				Pagination: &dcspb.Pagination{
					QueryId:     p.QueryId,
					ResultPages: int64(p.ResultPages),
				},
			},
		}, nil

	default: // match
		var m sourcebackendpb.Match
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return &dcspb.Event{
			Data: &dcspb.Event_Match{
				Match: &m,
			},
		}, nil
	}
	return nil, fmt.Errorf("unhandled type %q (data %q)", messageType.Type, string(data))
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()

	// Initialize the global tracer as early as possible:
	// common.Init uses gRPC.
	cfg := jaegercfg.Configuration{
		Sampler: &jaegercfg.SamplerConfig{
			Type:  "const",
			Param: 1,
		},
		Reporter: &jaegercfg.ReporterConfig{
			BufferFlushInterval: 1 * time.Second,
			LocalAgentHostPort:  *jaegerAgent,
		},
	}
	tracer, closer, err := cfg.New(
		"dcs-web",
		jaegercfg.Logger(jaeger.StdLogger),
	)
	if err != nil {
		log.Fatal(err)
	}
	opentracing.SetGlobalTracer(tracer)
	defer closer.Close()

	common.Init(*tlsCertPath, *tlsKeyPath, *staticPath)

	if *accessLogPath != "" {
		var err error
		accessLog, err = os.OpenFile(*accessLogPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			log.Fatal(err)
		}
	}

	if *clickLogPath != "" {
		var err error
		clickLog, err = os.OpenFile(*clickLogPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			log.Fatal(err)
		}
	}

	fmt.Printf("Debian Code Search webapp, version %s\n", common.Version)

	health.StartChecking()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Check if a static file was requested with full name
		name := filepath.Join(*staticPath, r.URL.Path)
		if r.URL.Path == "/" {
			name = filepath.Join(*staticPath, "index.html")
		}
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

		if err := common.Templates.ExecuteTemplate(w, "index.html", map[string]interface{}{
			"criticalcss": common.CriticalCss,
			"version":     common.Version,
			"host":        r.Host,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})
	http.HandleFunc("/favicon.ico", http.NotFound)
	http.HandleFunc("/goroutinez", goroutinez.Goroutinez)
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
	http.HandleFunc("/track", Track)

	traced := http.NewServeMux()
	traced.HandleFunc("/search", Search)
	traced.HandleFunc("/events/", EventsHandler)
	traced.Handle("/instantws", websocket.Handler(InstantServer))
	traceHandler := nethttp.Middleware(tracer, traced)
	http.Handle("/events/", traceHandler)
	// TODO: find a way to trace /instantws calls — re-implement the
	// http.Hijacker interface in nethttp.Middleware?
	// http.Handle("/instantws", traceHandler)
	http.Handle("/instantws", websocket.Handler(InstantServer))
	http.Handle("/search", traceHandler)

	// Used by the service worker.
	http.HandleFunc("/placeholder.html", func(w http.ResponseWriter, r *http.Request) {
		if err := common.Templates.ExecuteTemplate(w, "placeholder.html", map[string]interface{}{
			"criticalcss": common.CriticalCss,
			"version":     common.Version,
			"host":        r.Host,
			"q":           "%q%",
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	http.Handle("/metrics", prometheus.Handler())

	if *listenAddressPlain != "" {
		go func() {
			log.Fatal(http.ListenAndServe(*listenAddressPlain, nil))
		}()
	}

	log.Fatal(grpcutil.ListenAndServeTLS(*listenAddress,
		*tlsCertPath,
		*tlsKeyPath,
		func(s *grpc.Server) {
			dcspb.RegisterDCSServer(s, &server{})
		}))
}
