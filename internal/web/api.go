package web

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Debian/dcs/internal/api"
	"github.com/Debian/dcs/internal/apikeys"
	"github.com/Debian/dcs/internal/httperr"
	"github.com/Debian/dcs/internal/mmap"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"
)

var (
	metricQueryLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "dcs",
			Subsystem: "openapi",
			Name:      "query_latency_ms",
			Help:      "histogram of API query latency (in milliseconds)",
			Buckets: []float64{
				1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
				15, 20, 25, 30, 35, 40, 45, 50, 55, 60, 65, 70, 75, 80, 85, 90, 95, 100,
				150, 200, 250, 300, 350, 400, 450, 500, 550, 600, 650, 700, 750, 800, 850, 900, 950, 1000,
				2000, 3000, 4000, 5000, 6000, 7000, 8000, 9000, 10000,
				20000, 30000, 40000, 50000, 60000, 70000, 80000, 90000, 100000,
				500000, 1000000,
			},
		},
		[]string{
			"source",
		})

	metricInflightQueries = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "dcs",
			Subsystem: "openapi",
			Name:      "inflight_queries",
			Help:      "number of queries currently being processed",
		},
		[]string{
			"source",
		})

	metricSuccessfulQueries = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "dcs",
			Subsystem: "openapi",
			Name:      "successful_queries",
			Help:      "counter of successful queries",
		},
		[]string{
			"source",
		})

	metricErroredQueries = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "dcs",
			Subsystem: "openapi",
			Name:      "errored_queries",
			Help:      "counter of errored queries",
		},
		[]string{
			"source",
		})
)

func init() {
	prometheus.MustRegister(metricQueryLatency)
	prometheus.MustRegister(metricInflightQueries)
	prometheus.MustRegister(metricSuccessfulQueries)
	prometheus.MustRegister(metricErroredQueries)
}

type resultWriter struct {
	perBackend []*mmap.File
	w          io.Writer
	enc        *json.Encoder
	msg        sourcebackendpb.SearchReply
}

func (rw *resultWriter) fromPointers(pointers sortedPointers) error {
	idx := 0
	for ptr, err := range pointers.All() {
		if err != nil {
			return err
		}
		mapping := rw.perBackend[ptr.backendidx]
		if err := proto.Unmarshal(mapping.Data[ptr.offset:ptr.offset+int64(ptr.length)], &rw.msg); err != nil {
			return err
		}
		if rw.msg.Type != sourcebackendpb.SearchReply_MATCH {
			continue
		}

		if idx > 0 {
			rw.w.Write([]byte{','})
		}
		idx++
		m := rw.msg.Match
		contextBefore := make([]string, 0, 2)
		if m.Ctxp2 != "" {
			contextBefore = append(contextBefore, m.Ctxp2)
		}
		if m.Ctxp1 != "" {
			contextBefore = append(contextBefore, m.Ctxp1)
		}
		contextAfter := make([]string, 0, 2)
		if m.Ctxn1 != "" {
			contextAfter = append(contextAfter, m.Ctxn1)
		}
		if m.Ctxn2 != "" {
			contextAfter = append(contextAfter, m.Ctxn2)
		}
		if err := rw.enc.Encode(&api.SearchResult{
			Package:       m.Package,
			Path:          m.Path,
			Line:          m.Line,
			Context:       m.Context,
			ContextBefore: contextBefore,
			ContextAfter:  contextAfter,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (rw *resultWriter) Close() error {
	rw.w.Write([]byte{']'})
	for _, mapping := range rw.perBackend {
		if err := mapping.Close(); err != nil {
			return err
		}
	}
	return nil
}

func perBackendFromState(state *queryState) ([]*mmap.File, error) {
	perBackend := make([]*mmap.File, len(state.perBackend))
	for idx, state := range state.perBackend {
		mapping, err := mmap.Map(state.tempFile)
		if err != nil {
			return nil, err
		}
		perBackend[idx] = mapping
	}
	return perBackend, nil
}

func resultWriterFor(w io.Writer, state *queryState) (*resultWriter, error) {
	perBackend, err := perBackendFromState(state)
	if err != nil {
		return nil, err
	}

	w.Write([]byte{'['})

	return &resultWriter{
		perBackend: perBackend,
		w:          w,
		enc:        json.NewEncoder(w),
	}, nil
}

func writeSearchResults(w io.Writer, s *queryState) error {
	rw, err := resultWriterFor(w, s)
	if err != nil {
		return err
	}
	defer rw.Close()
	s.mu.Lock()
	resultPointers := s.resultPointers
	s.mu.Unlock()
	if err := rw.fromPointers(resultPointers); err != nil {
		return err
	}
	return nil
}

func writePerPackageSearchResults(w io.Writer, s *queryState) error {
	rw, err := resultWriterFor(w, s)
	if err != nil {
		return err
	}
	defer rw.Close()
	s.mu.Lock()
	pkgs := s.allPackagesSorted
	byPkg := s.resultPointersByPkg
	s.mu.Unlock()
	for idx, pkg := range pkgs {
		if idx == 0 {
			fmt.Fprintf(w, `{"package": "%s", "results":[`, pkg)
		} else {
			fmt.Fprintf(w, `,{"package": "%s", "results":[`, pkg)
		}

		if err := rw.fromPointers(memPointers(byPkg[pkg])); err != nil {
			return err
		}
		w.Write([]byte{']', '}'})
	}
	return nil
}

type apiserver struct {
	opts    *Opts
	decoder *apikeys.Decoder
}

func (a *apiserver) runAndWaitFor(ctx context.Context, cl client, srcLabel prometheus.Labels, q, continuationToken string) (*queryState, error) {
	queryid := queryID(q)

	metricInflightQueries.With(srcLabel).Inc()
	defer metricInflightQueries.With(srcLabel).Dec()
	log.Printf("api(%q, %q, %q)\n", queryid, cl.String(), q)

	if err := validateQuery("?" + q); err != nil {
		logRejected(cl, q, err)
		return nil, httperr.Error(http.StatusBadRequest, fmt.Errorf("Invalid query: %v (see /grep for usage)", err))
	}

	var s *queryState
	var err error
	if continuationToken != "" {
		// For valid speedbump continuation tokens,
		// start the query in exhaustive mode.
		var signed string
		if err := a.opts.speedbumpCookie.Decode("speedbump", continuationToken, &signed); err != nil || signed != q {
			// Otherwise (e.g. for expired continuation tokens),
			// tell the user they need to start from scratch.
			return nil, httperr.Error(http.StatusBadRequest, fmt.Errorf("invalid or expired continuation_token, please start over"))
		}
		s, _, err = a.opts.maybeStartQuery(ctx, queryid, cl, q)
	} else {
		// No continuation token specified (initial attempt):
		if cl.entry != "/grep" && anonymousKey(cl.apiKey) {
			// Keep old tools like codesearch-cli working as-is.
			// TODO: Remove this exemption once the tools
			// are updated to expose the speedbump properly.
			s, _, err = a.opts.maybeStartQuery(ctx, queryid, cl, q)
		} else {
			s, _, err = a.opts.maybeStartQuerySpeedbumped(ctx, cl, q, false)
		}
	}

	if err != nil {
		if errors.Is(err, errTooManyConcurrentQueries) ||
			errors.Is(err, errTooManyQueriesPerSecond) {
			err = maybeNudgeToApikey(cl.personalKey(), err)
			return nil, httperr.Error(http.StatusTooManyRequests, err)
		}
		return nil, fmt.Errorf("Could not start query: %v", err)
	}

	select {
	case <-s.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	s.mu.Lock()
	errs := s.errors
	s.mu.Unlock()
	if len(errs) > 0 {
		return nil, fmt.Errorf("search failed: %v", errs[0])
	}

	metricSuccessfulQueries.With(srcLabel).Inc()

	log.Printf("[%s] serving API results\n", queryid)

	s.mu.Lock()
	latency := time.Since(s.started)
	metricQueryLatency.With(srcLabel).Observe(float64(latency.Milliseconds()))
	s.mu.Unlock()
	return s, nil
}

func maybeNudgeToApikey(personalKey bool, err error) error {
	if personalKey {
		return err // no nudge needed
	}
	return fmt.Errorf("%w. With a personal API key, you get a limit of your own, see /apikeys/", err)
}

func (a *apiserver) common(w http.ResponseWriter, r *http.Request, writeResults func(io.Writer, *queryState) error) (err error) {
	ctx := r.Context()

	// Cross-Origin API requests are allowed.
	w.Header().Set("Allow", "OPTIONS, GET")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "OPTIONS, GET")
	w.Header().Set("Access-Control-Allow-Headers", "x-dcs-apikey, Content-Type")
	w.Header().Set("Access-Control-Expose-Headers", "X-Codesearch-Candidates, X-Codesearch-Results, X-Codesearch-Packages, X-Codesearch-Speedbump, X-Codesearch-Continuation-Token, Retry-After")
	w.Header().Set("Access-Control-Max-Age", "86400")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	var subject string
	if apikey := r.Header.Get("x-dcs-apikey"); apikey != "" {
		key, err := a.decoder.Decode(apikey)
		if err != nil {
			return httperr.Error(http.StatusForbidden, errors.New("invalid x-dcs-apikey header"))
		}
		subject = key.Subject
	}

	cl := clientFromRequest(r, "/api", subject)
	srcLabel := prometheus.Labels{"source": cmp.Or(subject, "anonymous")}
	defer func() {
		if err != nil && ctx.Err() == nil {
			metricErroredQueries.With(srcLabel).Inc()
		}
	}()

	query := r.FormValue("query")
	if query == "" {
		return httperr.Error(http.StatusBadRequest, errors.New("no query parameter specified (see /grep for usage)"))
	}

	literal := "0"
	if r.FormValue("literal") == "1" {
		literal = "1"
	}
	switch r.FormValue("match_mode") {
	case "literal":
		literal = "1"
	case "regex", "regexp":
		literal = "0"
	}

	// We encode a URL that contains _only_ the q parameter.
	q := url.Values{"q": []string{query}}.Encode() + "&literal=" + literal

	s, err := a.runAndWaitFor(ctx, cl, srcLabel, q, r.FormValue("continuation_token"))
	if err != nil {
		return err
	}
	s.mu.Lock()
	candidatesTotal := 0
	for _, total := range s.filesTotal {
		candidatesTotal += total
	}
	w.Header().Set("X-Codesearch-Candidates", strconv.Itoa(candidatesTotal))
	w.Header().Set("X-Codesearch-Results", strconv.Itoa(s.numResults))
	w.Header().Set("X-Codesearch-Packages", strconv.Itoa(len(s.allPackagesSorted)))
	if s.speedbump != nil {
		w.Header().Set("X-Codesearch-Speedbump", s.speedbump.Message)
		w.Header().Set("X-Codesearch-Continuation-Token", s.speedbump.ContinuationToken)
	}
	s.mu.Unlock()

	startJsonResponse(w)
	if err := writeResults(w, s); err != nil {
		return err
	}

	return nil
}

func (a *apiserver) search(w http.ResponseWriter, r *http.Request) error {
	return a.common(w, r, writeSearchResults)
}

func (a *apiserver) searchperpackage(w http.ResponseWriter, r *http.Request) error {
	return a.common(w, r, writePerPackageSearchResults)
}

func (o *Opts) serveAPIOnMux(mux *http.ServeMux, apiOpts apikeys.Options) error {
	a := &apiserver{
		opts: o,
		decoder: &apikeys.Decoder{
			SecureCookie: apiOpts.SecureCookie(),
		},
	}
	mux.Handle("GET /grep", httperr.ErrorHandler(a.grep))
	apiUsage := httperr.ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
		io.WriteString(w, "For scripts, bots and coding agents: /grep is the (recommended, preferred) plain-text API.\n")
		io.WriteString(w, "See /openapi2.yaml for OpenAPI (REST)\n")
		return nil
	})
	mux.Handle("GET /api", apiUsage)
	mux.Handle("GET /api/", apiUsage)
	// OPTIONS requests are used for preflight.
	mux.Handle("GET /api/v1/search", httperr.ErrorHandler(a.search))
	mux.Handle("OPTIONS /api/v1/search", httperr.ErrorHandler(a.search))
	mux.Handle("GET /api/v1/searchperpackage", httperr.ErrorHandler(a.searchperpackage))
	mux.Handle("OPTIONS /api/v1/searchperpackage", httperr.ErrorHandler(a.searchperpackage))
	return nil
}
