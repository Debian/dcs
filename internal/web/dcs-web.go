package web

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	_ "net/http/pprof"
	"net/url"
	"os"
	"path"
	"regexp"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Debian/dcs/internal/apikeys"
	"github.com/Debian/dcs/internal/grpcutil"
	"github.com/Debian/dcs/internal/index"
	"github.com/Debian/dcs/internal/proto/dcspb"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
	dcsregexp "github.com/Debian/dcs/internal/regexp"
	_ "github.com/Debian/dcs/internal/varz"
	"github.com/Debian/dcs/internal/version"
	"github.com/Debian/dcs/internal/web/common"
	"github.com/Debian/dcs/internal/web/health"
	"github.com/Debian/dcs/internal/web/search"
	"github.com/Debian/dcs/internal/web/show"
	"github.com/Debian/dcs/static"
	"github.com/gorilla/securecookie"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	_ "golang.org/x/net/trace"
	"google.golang.org/protobuf/proto"
)

var (
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
	if rewritten.Query().Get("literal") == "1" {
		return nil // not a regular expression
	}
	log.Printf("rewritten query = %q\n", rewritten.String())
	re, err := dcsregexp.Compile(rewritten.Query().Get("q"))
	if err != nil {
		return err
	}
	indexQuery := index.RegexpQuery(re.Syntax)
	log.Printf("trigram = %v, sub = %v", indexQuery.Trigram, indexQuery.Sub)
	if len(indexQuery.Trigram) == 0 && len(indexQuery.Sub) == 0 {
		return fmt.Errorf("Empty index query. See https://codesearch.debian.net/faq#emptyindex")
	}
	return nil
}

func (o *Opts) EventsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.FormValue("q")
	if query == "" {
		query = strings.TrimPrefix(r.URL.Path, "/events/")
	}
	w.Header().Set("Content-Type", "text/event-stream")

	// The additional ":" at the end is necessary so that we don’t need to
	// distinguish between the two cases (X-Forwarded-For, without a port, and
	// RemoteAddr, with a part) in the code below.
	src := r.Header.Get("X-Forwarded-For") + ":"
	if src == ":" || (!strings.HasPrefix(r.RemoteAddr, "[::1]:") &&
		!strings.HasPrefix(r.RemoteAddr, "127.0.0.1:")) {
		src = r.RemoteAddr
	}
	literal := r.FormValue("literal")
	if literal == "" {
		literal = "0"
	}
	q := "q=" + url.QueryEscape(query) + "&literal=" + literal

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

	cached, err := o.maybeStartQuery(ctx, identifier, src, q)
	if err != nil {
		log.Printf("[%s] could not start query: %+v\n", src, err)
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
		message, sequence, ok := getEvent(identifier, lastseen)
		if !ok {
			log.Printf("[%s] query no longer exists", src)
			return
		}
		lastseen = sequence
		// This message was obsoleted by a more recent one, e.g. a more
		// recent progress update obsoletes all earlier progress updates.
		if message.obsolete.Load() {
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
		stateMu.RLock()
		s, ok := state[queryid]
		var packages []string
		if ok {
			packages = s.allPackagesSorted
		}
		stateMu.RUnlock()
		if !ok {
			http.Error(w, "No such query.", http.StatusNotFound)
			return
		}

		if matches[2] == "json" {
			startJsonResponse(w)
		}

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
	stateMu.RLock()
	_, ok := state[queryid]
	stateMu.RUnlock()
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

type server struct {
	// For forward compatibility
	dcspb.UnimplementedDCSServer

	opts    *Opts
	decoder *apikeys.Decoder
}

func (s *server) Search(req *dcspb.SearchRequest, stream dcspb.DCS_SearchServer) error {
	ctx := stream.Context()
	query := req.GetQuery()

	key, err := s.decoder.Decode(req.GetApikey())
	if err != nil {
		return status.Errorf(codes.Unauthenticated, "invalid x-dcs-apikey header; please see https://codesearch.debian.net/apikeys/")
	}

	src := key.Subject + "@gRPC" // TODO: get remote address
	literal := "0"
	if req.GetLiteral() {
		literal = "1"
	}
	q := "q=" + url.QueryEscape(query) + "&literal=" + literal

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

	cached, err := s.opts.maybeStartQuery(ctx, identifier, src, q)
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
		message, sequence, ok := getEvent(identifier, lastseen)
		if !ok {
			return fmt.Errorf("query no longer exists")
		}
		lastseen = sequence
		// This message was obsoleted by a more recent one, e.g. a more
		// recent progress update obsoletes all earlier progress updates.
		if message.obsolete.Load() {
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

func (s *server) Results(req *dcspb.ResultsRequest, stream dcspb.DCS_ResultsServer) error {
	// TODO: de-dup with Search

	queryid := req.GetQueryId()

	key, err := s.decoder.Decode(req.GetApikey())
	if err != nil {
		return status.Errorf(codes.Unauthenticated, "invalid x-dcs-apikey header; please see https://codesearch.debian.net/apikeys/")
	}

	src := key.Subject + "@gRPC" // TODO: get remote address

	log.Printf("Results(queryid=%s, src=%s)", queryid, src)

	stateMu.RLock()
	state, ok := state[queryid]
	stateMu.RUnlock()
	if !ok {
		// TODO: canonical code
		return fmt.Errorf("not found")
	}

	perBackend, err := perBackendFromState(state)
	if err != nil {
		return err
	}

	var msg sourcebackendpb.SearchReply
	for _, ptr := range state.resultPointers {
		mapping := perBackend[ptr.backendidx]
		if err := proto.Unmarshal(mapping.Data[ptr.offset:ptr.offset+int64(ptr.length)], &msg); err != nil {
			return err
		}
		if msg.Type != sourcebackendpb.SearchReply_MATCH {
			continue
		}
		if err := stream.Send(msg.Match); err != nil {
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
}

type Opts struct {
	Mux                  *http.ServeMux
	ListenAddressPlain   string
	ListenAddress        string
	MemProfile           string
	AccessLogPath        string
	TLSCertPath          string
	TLSKeyPath           string
	TLSRequireClientAuth bool
	HashKeyStr           string
	BlockKeyStr          string
	ClickLogPath         string
	ClientID             string
	ClientSecret         string
	RedirectURL          string
	PrintVersion         bool
	SourceBackends       string
	UseSourcesDebianNet  bool
	QueryResultsPath     string
	CriticalCSS          []byte
}

func (o *Opts) Main(ln net.Listener) error {
	if o.PrintVersion {
		fmt.Printf("dcs-web version %s\n", version.Read())
		return nil
	}

	criticalCSS := o.CriticalCSS
	if criticalCSS == nil {
		var err error
		criticalCSS, err = fs.ReadFile(static.FS, "critical.min.css")
		if err != nil {
			return fmt.Errorf("critical.min.css not found (did you not run make static?)")
		}
	}
	common.CriticalCss = template.CSS(string(criticalCSS))
	common.Init(o.TLSCertPath, o.TLSKeyPath, o.SourceBackends, templates)

	if o.HashKeyStr == "" {
		return fmt.Errorf("-securecookie_hash_key is required. E.g.: -securecookie_hash_key=%x", securecookie.GenerateRandomKey(32))
	}

	hashKey, err := hex.DecodeString(o.HashKeyStr)
	if err != nil {
		return err
	}

	if o.BlockKeyStr == "" {
		return fmt.Errorf("-securecookie_block_key is required. E.g.: -securecookie_block_key=%x", securecookie.GenerateRandomKey(32))
	}

	blockKey, err := hex.DecodeString(o.BlockKeyStr)
	if err != nil {
		return err
	}

	if o.AccessLogPath != "" {
		var err error
		accessLog, err = os.OpenFile(o.AccessLogPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			return err
		}
	}

	if o.ClickLogPath != "" {
		var err error
		clickLog, err = os.OpenFile(o.ClickLogPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			return err
		}
	}

	fmt.Printf("Debian Code Search webapp, version %s\n", version.Read())

	health.StartChecking(o.UseSourcesDebianNet)

	mux := o.Mux
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Check if a static file was requested with full name
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if r.URL.Path == "/" {
			name = "index.html"
		}
		if _, err := fs.Stat(static.FS, name); err == nil {
			http.ServeFileFS(w, r, static.FS, name)
			return
		}

		// Or maybe /faq, which resolves to /faq.html
		name = name + ".html"
		if _, err := fs.Stat(static.FS, name); err == nil {
			http.ServeFileFS(w, r, static.FS, name)
			return
		}

		if err := common.Templates.ExecuteTemplate(w, "index.html", map[string]any{
			"criticalcss": common.CriticalCss,
			"version":     version.Read(),
			"host":        r.Host,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})
	mux.HandleFunc("/favicon.ico", http.NotFound)
	mux.HandleFunc("/show", show.Show(o.UseSourcesDebianNet))
	mux.HandleFunc("/memprof", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("writing memprof")
		if o.MemProfile != "" {
			f, err := os.Create(o.MemProfile)
			if err != nil {
				log.Printf("%v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			pprof.WriteHeapProfile(f)
			f.Close()
			return
		}
	})

	mux.HandleFunc("/results/", ResultsHandler)
	mux.HandleFunc("/perpackage-results/", o.PerPackageResultsHandler)
	mux.HandleFunc("/queryz", QueryzHandler)
	mux.HandleFunc("/track", Track)

	traced := http.NewServeMux()
	traced.HandleFunc("/search", o.Search)
	traced.HandleFunc("/events/", o.EventsHandler)
	mux.Handle("/events/", traced)
	mux.Handle("/search", traced)

	// Used by the service worker.
	mux.HandleFunc("/placeholder.html", func(w http.ResponseWriter, r *http.Request) {
		if err := common.Templates.ExecuteTemplate(w, "placeholder.html", map[string]any{
			"criticalcss": common.CriticalCss,
			"version":     version.Read(),
			"host":        r.Host,
			"q":           "%q%",
			"literal":     true,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	mux.Handle("/metrics", promhttp.Handler())

	apiOpts := apikeys.Options{
		HashKey:      hashKey,
		BlockKey:     blockKey,
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		RedirectURL:  o.RedirectURL,
		Prefix:       "/apikeys",
	}
	{
		apiMux := http.NewServeMux()
		mux.Handle("/api/", http.StripPrefix("/api", apiMux))
		if err := o.serveAPIOnMux(apiMux, apiOpts); err != nil {
			return err
		}
	}

	// Initialize the /apikeys/ functionality asynchronously, so that a salsa
	// outage does not take down DCS:
	{
		apiKeysMux := http.NewServeMux()
		mux.Handle("/apikeys/", http.StripPrefix("/apikeys", apiKeysMux))
		go func() {
			for {
				if err := apikeys.ServeOnMux(apiKeysMux, apiOpts); err != nil {
					log.Printf("cannot serve /apikeys/: %v", err)
					time.Sleep(10 * time.Second)
					continue
				}
				break
			}
		}()
	}

	// Handled by nginx in production, so this is just for testing
	{
		u, err := url.Parse("https://codesearch.debian.net")
		if err != nil {
			return err
		}
		mux.Handle("/apidocs/", httputil.NewSingleHostReverseProxy(u))
	}

	if o.ListenAddressPlain != "" {
		go func() {
			log.Fatal(http.ListenAndServe(o.ListenAddressPlain, mux))
		}()
	}

	return grpcutil.ListenAndServeTLS(ln,
		mux,
		o.TLSCertPath,
		o.TLSKeyPath,
		o.TLSRequireClientAuth,
		func(s *grpc.Server) {
			decoder := &apikeys.Decoder{
				SecureCookie: apiOpts.SecureCookie(),
			}
			dcspb.RegisterDCSServer(s, &server{
				opts:    o,
				decoder: decoder,
			})
		})
}
