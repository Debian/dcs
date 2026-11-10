package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/Debian/dcs/internal/frequency"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
	"github.com/Debian/dcs/internal/stringpool"
	"github.com/Debian/dcs/internal/web/common"
	"github.com/Debian/dcs/internal/web/search"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/semaphore"
	"google.golang.org/protobuf/proto"
	"pault.ag/go/debian/version"
)

var (
	perPackagePathRe = regexp.MustCompile(`^/perpackage-results/([^/]+)/` +
		strconv.Itoa(resultsPerPackage) + `/page_([0-9]+).json$`)

	queryDurations = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "query_durations_ms",
			Help: "Duration of a query in milliseconds.",
			Buckets: []float64{
				1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
				15, 20, 25, 30, 35, 40, 45, 50, 55, 60, 65, 70, 75, 80, 85, 90, 95, 100,
				150, 200, 250, 300, 350, 400, 450, 500, 550, 600, 650, 700, 750, 800, 850, 900, 950, 1000,
				2000, 3000, 4000, 5000, 6000, 7000, 8000, 9000, 10000,
				20000, 30000, 40000, 50000, 60000, 70000, 80000, 90000, 100000,
				500000, 1000000,
			},
		})

	headroomPercentage = flag.Float64("headroom_percentage",
		0.2,
		"How much space should be kept free on the file system containing -query_results_path in order to be able to write query state. Default: 0.2, i.e. 20% of the total space should be kept free. Set to 0 to disable")
)

const (
	// NB: All of these constants needs to match those in static/instant.js.
	packagesPerPage   = 5
	resultsPerPackage = 2
	resultsPerPage    = 10
)

func init() {
	prometheus.MustRegister(queryDurations)
}

type Error struct {
	// This is set to “error” to distinguish the message type on the client.
	Type string

	// Currently only “backendunavailable”
	ErrorType string
}

type ProgressUpdate struct {
	Type           string
	QueryId        string
	FilesProcessed int
	FilesTotal     int
	Results        int
	ReadBytes      uint64 // cumulative, for displaying read throughput
}

func (p *ProgressUpdate) EventType() string {
	return p.Type
}

func (p *ProgressUpdate) ObsoletedBy(newEvent *obsoletableEvent) bool {
	return (*newEvent).EventType() == p.Type
}

type Queued struct {
	Type  string // “queued”
	Ahead int    // number of queries in front
}

type resultPointer struct {
	backendidx uint32
	// Used for per-package results. Indexes into a stringpool.StringPool
	packageIdx uint32
	ranking    float32
	length     uint32
	offset     int64

	// Used as a tie-breaker when sorting by ranking to guarantee stable
	// results, independent of the order in which the results are returned from
	// source backends.
	pathHash uint64
}

func (rp *resultPointer) Marshal(b *[32]byte) {
	binary.LittleEndian.PutUint32(b[0:], rp.backendidx)
	binary.LittleEndian.PutUint32(b[4:], rp.packageIdx)
	binary.LittleEndian.PutUint32(b[8:], math.Float32bits(rp.ranking))
	binary.LittleEndian.PutUint32(b[12:], rp.length)
	binary.LittleEndian.PutUint64(b[16:], uint64(rp.offset))
	binary.LittleEndian.PutUint64(b[24:], rp.pathHash)
}

func (rp *resultPointer) Unmarshal(b *[32]byte) {
	rp.backendidx = binary.LittleEndian.Uint32(b[0:])
	rp.packageIdx = binary.LittleEndian.Uint32(b[4:])
	rp.ranking = math.Float32frombits(binary.LittleEndian.Uint32(b[8:]))
	rp.length = binary.LittleEndian.Uint32(b[12:])
	rp.offset = int64(binary.LittleEndian.Uint64(b[16:]))
	rp.pathHash = binary.LittleEndian.Uint64(b[24:])
}

type pointerByRanking []resultPointer

func (s pointerByRanking) Len() int {
	return len(s)
}

func (s pointerByRanking) Less(i, j int) bool {
	a, b := &s[i], &s[j]
	// Sort by ranking, primarily.
	if a.ranking != b.ranking {
		return a.ranking > b.ranking
	}
	// For equal ranking, break via the pathHash.
	if a.pathHash != b.pathHash {
		return a.pathHash > b.pathHash
	}
	// For equal path hash, break via backendidx and offset.
	if a.backendidx != b.backendidx {
		return a.backendidx > b.backendidx
	}
	return a.offset < b.offset
}

func (s pointerByRanking) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

type perBackendState struct {
	// One file per backend, containing JSON-serialized results. When writing,
	// we keep the offsets, so that we can later sort the pointers and write
	// the resulting files.
	tempFile       *os.File
	tempFileWriter *bufio.Writer
	tempFileOffset int64
	packagePool    *stringpool.StringPool
	allPackages    map[string]bool
	stats          *sourcebackendpb.SearchStats
}

var processStart = time.Now()

type queryState struct {
	// config (never changed)
	queryid  string
	query    string
	client   client
	request  *sourcebackendpb.SearchRequest
	started  time.Time
	newEvent *sync.Cond

	// This guards concurrent access to any perBackend[].tempFile.
	tempFilesMu sync.Mutex
	perBackend  []*perBackendState

	// state
	done chan struct{} // closed when done
	// lastAccess is time.Since(processStart),
	// for which Go uses a monotonic clock:
	// https://golang.org/design/12914-monotonic
	lastAccess atomic.Int64
	// state (protected by mu)
	mu          sync.Mutex
	events      []event
	firstResult time.Time
	ended       time.Time
	released    bool    // evicted (what a failed state map lookup used to mean)
	errors      []error // one error per failed backend
	cancel      context.CancelFunc
	cancelled   bool
	queued      time.Duration

	results [10]resultPointer

	filesTotal     []int
	filesProcessed []int
	readBytes      []uint64

	resultPages int

	numResults          int
	resultWriter        diskWriter
	resultPointers      sortedPointers
	resultPointersByPkg map[string][]resultPointer

	allPackagesSorted []string

	FirstPathRank float32
}

type sink struct {
	ctx context.Context // for respecting cancellation

	opts       *Opts
	queryState *queryState
	backendidx int

	// mu guards the perBackend.tempFileWriter
	mu sync.Mutex
}

// Match implements sourcebackend.SearchSink.
func (s *sink) Match(m *sourcebackendpb.Match) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}

	b, err := proto.Marshal(&sourcebackendpb.SearchReply{
		Type:  sourcebackendpb.SearchReply_MATCH,
		Match: m,
	})
	if err != nil {
		return fmt.Errorf("encoding proto: %v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	bstate := s.queryState.perBackend[s.backendidx]
	if _, err := bstate.tempFileWriter.Write(b); err != nil {
		return fmt.Errorf("writing proto: %v", err)
	}

	if err := s.queryState.storeResult(s.backendidx, m, len(b)); err != nil {
		return fmt.Errorf("writing result: %v", err)
	}

	bstate.tempFileOffset += int64(len(b))
	if s.queryState.completed() {
		return fmt.Errorf("query was cancelled")
	}

	return nil
}

// Progress implements sourcebackend.SearchSink.
func (s *sink) Progress(filesProcessed, filesTotal int, readBytes uint64) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}

	orderlyFinished := filesProcessed == filesTotal
	if orderlyFinished {
		// Flush before storeProgress so that the file is on disk
		// before clients start requesting it.
		s.mu.Lock()
		bstate := s.queryState.perBackend[s.backendidx]
		bstate.tempFileWriter.Flush()
		s.mu.Unlock()
	}
	s.opts.storeProgress(s.queryState, s.backendidx, &sourcebackendpb.ProgressUpdate{
		FilesProcessed: uint64(filesProcessed),
		FilesTotal:     uint64(filesTotal),
		ReadBytes:      readBytes,
	})
	if s.queryState.completed() && filesProcessed < filesTotal {
		return fmt.Errorf("query was cancelled")
	}
	return nil
}

// Stats implements sourcebackend.SearchSink
func (s *sink) Stats(st *sourcebackendpb.SearchStats) error {
	s.queryState.mu.Lock()
	defer s.queryState.mu.Unlock()
	s.queryState.perBackend[s.backendidx].stats = st
	return nil
}

func (o *Opts) queryBackend(ctx context.Context, s *queryState, src string, backend common.Backend, backendidx int, searchRequest *sourcebackendpb.SearchRequest) error {
	ctx, cancelfunc := context.WithCancel(ctx)
	defer cancelfunc()
	err := backend.Search(ctx, searchRequest, &sink{
		ctx:        ctx,
		opts:       o,
		queryState: s,
		backendidx: backendidx,
	})
	s.perBackend[backendidx].tempFileWriter.Flush()
	if err != nil {
		log.Printf("[%s] [src:%s] Search RPC failed: %v\n", s.queryid, src, err)
		return err
	}
	return nil
}

func (s *queryState) expired() bool {
	return time.Since(s.started) > 30*time.Minute
}

func releaseQueryLocked(s *queryState) {
	s.mu.Lock()
	s.released = true
	if !s.completed() {
		close(s.done)
	}
	s.newEvent.Broadcast() // unblock getEvent
	s.mu.Unlock()
	for _, state := range s.perBackend {
		state.tempFile.Close()
	}
}

func (o *Opts) lookupQuery(queryid string) (*queryState, bool) {
	o.queriesMu.RLock()
	defer o.queriesMu.RUnlock()
	s, ok := o.queries[queryid]
	if ok {
		s.touch()
	}
	return s, ok
}

const (
	// maxQueries is the upper limit of how many queries DCS keeps.
	// Each finished query keeps one results file on disk and <= 64 MB of RAM.
	maxQueries = 300

	// maxQueriesPerOwner is the upper limit of how many queries DCS keeps
	// per owner, i.e. API key owner, or IP address for anonymous access,
	// when maxQueries is reached.
	maxQueriesPerOwner = 10
)

func (o *Opts) startQuery(querystate *queryState) (*queryState, bool) {
	o.queriesMu.Lock()
	defer o.queriesMu.Unlock()
	if s, ok := o.queries[querystate.queryid]; ok {
		if !s.expired() {
			// This query is already active, do not interfere.
			return s, true
		}
		// Prepare for reusing the query slot by releasing resources.
		releaseQueryLocked(s)
	} else {
		// See if we need to garbage collect old queries. This is unnecessary when
		// the query is expired, as we can just re-use the previous slot.
		if len(o.queries) >= maxQueries {
			log.Printf("Trying to garbage collect queries (currently %d)", len(o.queries))
			candidates := slices.Collect(maps.Values(o.queries))
			sort.Slice(candidates, func(i, j int) bool {
				return candidates[i].lastAccess.Load() < candidates[j].lastAccess.Load()
			})
			// Split candidates into own (same owner) and others,
			// so that the owner’s queries are first considered for GC.
			var own, others []*queryState
			owner := querystate.client.owner()
			for _, s := range candidates {
				if s.client.owner() == owner {
					own = append(own, s)
				} else {
					others = append(others, s)
				}
			}
			if len(own) >= maxQueriesPerOwner {
				candidates = append(own, others...)
			}
			for _, s := range candidates {
				if len(o.queries) < maxQueries {
					break
				}
				if !s.completed() {
					continue
				}
				releaseQueryLocked(s)
				delete(o.queries, s.queryid)
				if err := os.RemoveAll(s.resultWriter.dir); err != nil {
					log.Printf("[%s] removing query results: %v", s.queryid, err)
				}
			}
			log.Printf("Garbage collection done. %d queries remaining", len(o.queries))
		}
	}
	o.queries[querystate.queryid] = querystate
	activeQueries.Add(1)
	frequency.IncUsers()
	querystate.touch()
	return querystate, false
}

// XXX: Starting a new query while there may still be clients reading that
// query is not a great idea. Best fix may be to make getEvent() use a
// querystate instead of the string identifier.

var errTooManyQueries = errors.New("too many concurrent queries, see https://codesearch.debian.net/faq#throttled")

// maybeStartQuery starts a specified query if that query does not already
// exist. Returns the *queryState and whether the query existed.
func (o *Opts) maybeStartQuery(ctx context.Context, queryid string, cl client, query string) (*queryState, bool, error) {
	if s, ok := o.lookupQuery(queryid); ok && !s.expired() {
		logReused(s, cl)
		return s, true, nil
	}

	releaseOwner := func() {}
	if o.maxQuerySem != nil {
		// Reject queries from this owner if this owner
		// has too many concurrent queries already.
		owner, unref := o.ownerSlot(cl.owner())
		if !owner.sem.TryAcquire(1) {
			unref()
			// Reject with HTTP 429 (Too Many Requests).
			logRejected(cl, query, errTooManyQueries)
			return nil, false, errTooManyQueries
		}
		releaseOwner = func() {
			owner.sem.Release(1)
			unref()
		}
	}
	started := false
	defer func() {
		if !started {
			releaseOwner()
		}
	}()

	// TODO: it’d be so much better if we would correctly handle ESPACE errors
	// in the code below (and above), but for that we need to carefully test it.
	o.ensureEnoughSpaceAvailable()

	dir := filepath.Join(o.QueryResultsPath, queryid)
	if err := os.MkdirAll(dir, os.FileMode(0755)); err != nil {
		return nil, false, fmt.Errorf("could not create %q: %w", dir, err)
	}

	// TODO: without this line, searches would fail too early.
	// Debug and figure out why.
	ctx = context.Background()

	querystate := &queryState{
		queryid:        queryid,
		started:        time.Now(),
		done:           make(chan struct{}),
		query:          query,
		client:         cl,
		filesTotal:     make([]int, len(o.backends)),
		filesProcessed: make([]int, len(o.backends)),
		readBytes:      make([]uint64, len(o.backends)),
		perBackend:     make([]*perBackendState, len(o.backends)),
		resultPointers: memPointers(nil),
		resultWriter: diskWriter{
			// NOTE: append overshoots by up to 25%, so the 64 MB here
			// will be exceeded by up to 25% in practice.
			flushThreshold: int(64 * 1024 * 1024 / unsafe.Sizeof(resultPointer{})),
			dir:            dir,
		},
	}
	querystate.newEvent = sync.NewCond(&querystate.mu)

	for i := 0; i < len(o.backends); i++ {
		querystate.filesTotal[i] = -1
		path := filepath.Join(dir, fmt.Sprintf("unsorted_%d.pb", i))
		f, err := os.Create(path)
		if err != nil {
			return nil, false, fmt.Errorf("could not create %q: %w", path, err)
		}
		querystate.perBackend[i] = &perBackendState{
			packagePool:    stringpool.NewStringPool(),
			tempFile:       f,
			tempFileWriter: bufio.NewWriterSize(f, 65536),
			allPackages:    make(map[string]bool),
		}
	}

	// Rewrite the query into a query for source backends.
	fakeUrl, err := url.Parse("?" + query)
	if err != nil {
		log.Fatal(err)
	}
	rewritten := search.RewriteQuery(*fakeUrl)
	searchRequest := &sourcebackendpb.SearchRequest{
		Query:        rewritten.Query().Get("q"),
		QueryId:      queryid,
		RewrittenUrl: rewritten.String(),
		Literal:      rewritten.Query().Get("literal") == "1",
	}
	querystate.request = searchRequest
	log.Printf("[%s] querying for %+v\n", queryid, searchRequest)
	if existing, ok := o.startQuery(querystate); ok {
		// Another goroutine must have raced us since we called lookupQuery().
		logReused(existing, cl)
		return existing, true, nil
	}
	started = true // runQuery calls releaseOwner
	go o.runQuery(ctx, querystate, cl.String(), searchRequest, releaseOwner)
	return querystate, false, nil
}

// logReused logs a request served by an existing (running or cached) query.
// Together with the "query done" lines (one per execution), there is exactly
// one structured line per query request.
func logReused(s *queryState, cl client) {
	slog.Info("query reused",
		"query_id", s.queryid,
		"request", s.query,
		"in_progress", !s.completed(),
		"client_ip", cl.ip,
		"user_agent", cl.userAgent,
		"entry", cl.entry,
		"apikey", cl.apiKey)
}

func logRejected(cl client, request string, err error) {
	slog.Info("query rejected",
		"request", request,
		"client_ip", cl.ip,
		"user_agent", cl.userAgent,
		"entry", cl.entry,
		"apikey", cl.apiKey,
		"error", err)
}

func logStarted(s *queryState, cl client, running int32) {
	slog.Info("query started",
		"query_id", s.queryid,
		"request", s.query,
		"client_ip", cl.ip,
		"user_agent", cl.userAgent,
		"entry", cl.entry,
		"apikey", cl.apiKey,
		"queued", s.queued.Round(time.Millisecond),
		"running", running)
}

func (o *Opts) ownerSlot(owner string) (_ *ownerSlot, unref func()) {
	o.ownersMu.Lock()
	defer o.ownersMu.Unlock()
	slot, ok := o.owners[owner]
	if !ok {
		slot = &ownerSlot{
			sem: semaphore.NewWeighted(int64(o.MaxConcurrentQueriesByOwner)),
		}
		o.owners[owner] = slot
	}
	slot.refs++
	return slot, func() {
		// Unref: clean up the entry after the last query finishes.
		o.ownersMu.Lock()
		defer o.ownersMu.Unlock()
		slot.refs--
		if slot.refs == 0 {
			delete(o.owners, owner)
		}
	}
}

func (o *Opts) admitOrQueue(ctx context.Context, s *queryState) (release func(), _ error) {
	release = func() { o.maxQuerySem.Release(1) }
	if o.maxQuerySem.TryAcquire(1) {
		return release, nil // admitted
	}

	// Semaphore could not be acquired,
	// i.e. no query slots are available. Queue.
	ahead := int(o.maxQueryWaiting.Add(1)) - 1
	defer o.maxQueryWaiting.Add(-1)
	log.Printf("[%s] waiting for a search slot (queries ahead: %d)", s.queryid, ahead)
	s.addEventMarshal(&Queued{
		Type: "queued",
		// Add 1 so that the Queued event always has Ahead>0
		// when a query is queued. This also avoids
		// the awkward special case of “You are queued,
		// there are 0 queries in front”, which can happen
		// when a query finishes just after this goroutine schedules
		// but before maxQueryWaiting is accessed.
		Ahead: 1 + ahead,
	})
	// Block until the semaphore can be acquired.
	if err := o.maxQuerySem.Acquire(ctx, 1); err != nil {
		// Context cancelled, i.e. the waiter gave up.
		return nil, err
	}
	return release, nil
}

func (o *Opts) runQuery(ctx context.Context, s *queryState, src string, searchRequest *sourcebackendpb.SearchRequest, releaseOwner func()) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	cancelled := s.cancelled // did cancellation race us?
	s.cancel = cancel
	s.mu.Unlock()
	if cancelled {
		cancel()
	}

	// Enforce query concurrency limit by queueing.
	if o.maxQuerySem != nil {
		release, err := o.admitOrQueue(ctx, s)
		if err != nil {
			s.mu.Lock()
			s.errors = append(s.errors, err)
			s.mu.Unlock()
			releaseOwner()
			o.finishQuery(s)
			return
		}
		defer release()
		s.mu.Lock()
		s.queued = time.Since(s.started)
		s.mu.Unlock()
	}

	running := o.running.Add(1)
	defer o.running.Add(-1)
	logStarted(s, s.client, running)

	// The query timeout starts after queueing.
	if timeout := o.QueryTimeout; timeout > 0 {
		// Shadow ctx, but not cancel
		var cancelTimeout context.CancelFunc
		ctx, cancelTimeout = context.WithTimeout(ctx, timeout)
		defer cancelTimeout()
	}

	var wg sync.WaitGroup
	for idx, backend := range o.backends {
		wg.Go(func() {
			if err := o.queryBackend(ctx, s, src, backend, idx, searchRequest); err != nil {
				s.mu.Lock()
				s.errors = append(s.errors, err)
				s.mu.Unlock()
			}
		})
	}
	wg.Wait()
	releaseOwner()
	o.finishQuery(s)
}

type queryStats struct {
	Searchterm     string
	QueryId        string
	NumEvents      int
	NumResults     int
	NumResultPages int
	NumPackages    int
	Done           bool
	Started        time.Time
	Ended          time.Time
	StartedFromNow time.Duration
	Duration       time.Duration
	FilesTotal     []int
	FilesProcessed []int
}

func (o *Opts) QueryzHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	if cancel := r.PostFormValue("cancel"); cancel != "" {
		if s, ok := o.lookupQuery(cancel); ok {
			s.mu.Lock()
			cancel := s.cancel
			s.cancelled = true
			s.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
		http.Redirect(w, r, "/queryz", http.StatusFound)
		return
	}

	o.queriesMu.RLock()
	stats := make([]queryStats, len(o.queries))
	idx := 0
	for queryid, s := range o.queries {
		s.mu.Lock()
		stats[idx] = queryStats{
			Searchterm:     s.query,
			QueryId:        queryid,
			NumEvents:      len(s.events),
			Done:           s.completed(),
			Started:        s.started,
			Ended:          s.ended,
			StartedFromNow: time.Since(s.started),
			Duration:       s.ended.Sub(s.started),
			NumResults:     s.numResults,
			NumResultPages: s.resultPages,
			FilesTotal:     slices.Clone(s.filesTotal),
			FilesProcessed: slices.Clone(s.filesProcessed),
		}
		if stats[idx].NumResults == 0 && stats[idx].Done {
			stats[idx].NumResults = s.numResults
		}
		s.mu.Unlock()
		idx++
	}
	o.queriesMu.RUnlock()

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Started.After(stats[j].Started)
	})

	if err := common.Templates.ExecuteTemplate(w, "queryz.html", map[string]any{
		"queries": stats,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Caller needs to hold s.clientsMu
func (s *queryState) sendPaginationUpdate() {
	type Pagination struct {
		// Set to “pagination”.
		Type        string
		QueryId     string
		ResultPages int
	}

	if s.resultPages > 0 {
		s.addEventMarshal(&Pagination{
			Type:        "pagination",
			QueryId:     s.queryid,
			ResultPages: s.resultPages,
		})
	}
}

func (s *queryState) touch() {
	s.lastAccess.Store(int64(time.Since(processStart)))
}

func (s *queryState) storeResult(backendidx int, result *sourcebackendpb.Match, resultLen int) error {
	h := fnv.New64()
	io.WriteString(h, result.Path)

	// Check if we need to consider this result for the top 10.
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.released {
		return fmt.Errorf("query %s no longer exists", s.queryid)
	}
	if s.firstResult.IsZero() {
		s.firstResult = time.Now()
	}

	if s.FirstPathRank == 0 {
		// This code path (and lock acquisition) gets executed only on the
		// first result.
		s.FirstPathRank = result.Pathrank
	}
	// Now store the combined ranking of PathRanking (pre) and Ranking (post).
	// We add the values because they are both percentages.
	// To make the Ranking (post) less significant, we multiply it with
	// 1/10 * FirstPathRank. We used to use maxPathRanking here, but
	// requiring that means delaying the search until all results are
	// there. Instead, FirstPathRank is a good enough approximation (but
	// different enough for each query that we can’t hardcode it).
	result.Ranking = result.Pathrank + ((s.FirstPathRank * 0.1) * result.Ranking)

	if result.Ranking > s.results[9].ranking {
		// TODO: find the first s.result[] for the same package. then check again if the result is worthy of replacing that per-package result
		// TODO: probably change the data structure so that we can do this more easily and also keep N results per package.

		combined := append(s.results[:], resultPointer{
			ranking:  result.Ranking,
			pathHash: h.Sum64(),
		})
		sort.Sort(pointerByRanking(combined))
		copy(s.results[:], combined[:10])
		// Temporarily unlock while writing the results to disk.
		s.mu.Unlock()

		// The result entered the top 10, so send it to the client(s) for
		// immediate display.
		// TODO: make this satisfy obsoletableEvent in order to skip
		// sending results to the client which are then overwritten by
		// better top10 results.
		b := bytes.Buffer{}
		if err := WriteMatchJSON(result, &b); err != nil {
			log.Fatalf("Could not marshal result as JSON: %v\n", err)
		}
		s.addEvent(b.Bytes(), &result)

		s.mu.Lock()
	}

	bstate := s.perBackend[backendidx]
	bstate.allPackages[result.Package] = true
	err := s.resultWriter.Add(resultPointer{
		backendidx: uint32(backendidx),
		ranking:    result.Ranking,
		offset:     bstate.tempFileOffset,
		length:     uint32(resultLen),
		pathHash:   h.Sum64(),
		packageIdx: bstate.packagePool.Intern(result.Package),
	})
	if err != nil {
		return err
	}
	s.numResults++
	return nil
}

func (o *Opts) finishQuery(s *queryState) {
	if s.completed() {
		return // already finished
	}

	s.mu.Lock()
	for idx := range s.filesTotal {
		filesTotal := s.filesTotal[idx]
		if filesTotal == -1 {
			filesTotal = 0
		}
		s.filesTotal[idx] = filesTotal
		s.filesProcessed[idx] = filesTotal
	}

	filesTotal := 0
	for _, total := range s.filesTotal {
		filesTotal += total
	}
	numResults := s.numResults
	var firstResult time.Duration
	if s.firstResult.IsZero() {
		// firstResult remains 0 for queries without results
	} else {
		firstResult = s.firstResult.Sub(s.started)
	}
	errors := s.errors
	cancelled := s.cancelled
	var (
		candidates uint64
		searched   uint64
		readBytes  uint64
		slowest    *sourcebackendpb.SearchStats
		indexMax   time.Duration
		indexSum   time.Duration
		grepMax    time.Duration
		grepSum    time.Duration
		missing    int
		timedOut   bool
		incomplete bool
	)
	for _, b := range s.perBackend {
		st := b.stats
		if st == nil {
			missing++ // backend failed
			continue
		}
		candidates += st.Candidates
		searched += st.Searched
		readBytes += st.ReadBytes
		if slowest == nil || st.SlowestNs > slowest.SlowestNs {
			slowest = st
		}
		if st.Cancelled {
			timedOut = true
		}
		if st.Incomplete {
			incomplete = true
		}
		indexMax = max(indexMax, time.Duration(st.IndexNs))
		indexSum += time.Duration(st.IndexNs)
		grepMax = max(grepMax, time.Duration(st.GrepNs))
		grepSum += time.Duration(st.GrepNs)
	}
	s.mu.Unlock()

	log.Printf("[%s] query done on all backends, writing to disk.\n", s.queryid)
	if err := o.writeToDisk(s); err != nil {
		log.Printf("[%s] writeToDisk() failed: %v\n", s.queryid, err)
		failedQueries.Inc()
		s.addEventMarshal(&Error{
			Type:      "error",
			ErrorType: "failed",
		})
	}

	outcome := "ok"
	switch {
	case cancelled:
		outcome = "cancelled"
		s.addEventMarshal(&Error{
			Type:      "error",
			ErrorType: "cancelled",
		})

	case len(errors) > 0:
		outcome = "error"
		s.addEventMarshal(&Error{
			Type:      "error",
			ErrorType: "backendunavailable",
		})

	case incomplete:
		outcome = "speedbump"

	case timedOut:
		outcome = "timeout"
	}

	s.addEventMarshal(&ProgressUpdate{
		Type:           "progress",
		QueryId:        s.queryid,
		FilesProcessed: filesTotal,
		FilesTotal:     filesTotal,
		Results:        numResults,
		ReadBytes:      readBytes,
	})

	slog.Info("query done",
		"query_id", s.queryid,
		"request", s.query,
		"query", s.request.GetQuery(),
		"literal", s.request.GetLiteral(),
		"rewritten_url", s.request.GetRewrittenUrl(),
		"case_insensitive", slowest.GetCaseInsensitive(),
		"positional", slowest.GetPositional(),
		"required_literals", slowest.GetRequiredLiterals(),
		"client_ip", s.client.ip,
		"user_agent", s.client.userAgent,
		"entry", s.client.entry,
		"apikey", s.client.apiKey,
		"first_result", firstResult.Round(time.Millisecond),
		"duration", time.Since(s.started).Round(time.Millisecond),
		"queued", s.queued.Round(time.Millisecond),
		"running", o.running.Load(),
		"outcome", outcome,
		"results", numResults,
		"searched", searched,
		"candidates", candidates,
		"read_bytes", readBytes,
		"slowest_path", slowest.GetSlowestPath(),
		"slowest", time.Duration(slowest.GetSlowestNs()),
		"index_max", indexMax.Round(time.Microsecond),
		"index_sum", indexSum.Round(time.Microsecond),
		"grep_max", grepMax.Round(time.Microsecond),
		"grep_sum", grepSum.Round(time.Microsecond),
		"missing_shards", missing)

	s.touch()
	s.addEvent([]byte{}, nil)

	queryDurations.Observe(float64((time.Since(s.started) - s.queued) / time.Millisecond))
}

func fsBytes(path string) (available uint64, total uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		log.Fatalf("Could not stat filesystem for %q: %v\n", path, err)
	}
	available = stat.Bavail * uint64(stat.Bsize)
	total = stat.Blocks * uint64(stat.Bsize)
	return available, total
}

// Makes sure 20% of the filesystem backing -query_results_path are available,
// cleans up old query results otherwise.
func (o *Opts) ensureEnoughSpaceAvailable() {
	if err := os.MkdirAll(o.QueryResultsPath, 0755); err != nil {
		log.Println(err)
	}
	available, total := fsBytes(o.QueryResultsPath)
	headroom := uint64(*headroomPercentage * float64(total))
	log.Printf("%d bytes available, %d bytes headroom required (20%%)\n", available, headroom)
	if available >= headroom {
		return
	}

	log.Printf("Deleting an old query...\n")
	dir, err := os.Open(o.QueryResultsPath)
	if err != nil {
		log.Fatal(err)
	}
	defer dir.Close()
	infos, err := dir.Readdir(-1)
	if err != nil {
		log.Fatal(err)
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].ModTime().Before(infos[j].ModTime())
	})
	for _, info := range infos {
		if !info.IsDir() {
			continue
		}
		log.Printf("Removing query results for %q to make enough space\n", info.Name())
		if err := os.RemoveAll(filepath.Join(o.QueryResultsPath, info.Name())); err != nil {
			log.Fatal(err)
		}
		available, _ = fsBytes(o.QueryResultsPath)
		if available >= headroom {
			break
		}
	}
}

func (s *queryState) writeFromPointers(f io.Writer, pointers []resultPointer) error {
	s.mu.Lock()
	firstPathRank := s.FirstPathRank
	s.mu.Unlock()

	s.tempFilesMu.Lock()
	defer s.tempFilesMu.Unlock()

	if _, err := f.Write([]byte("[")); err != nil {
		return err
	}
	var msg sourcebackendpb.SearchReply
	for idx, pointer := range pointers {
		src := s.perBackend[pointer.backendidx].tempFile
		// TODO: Avoid the allocations by using a slice and only allocate a new buffer when pointer.length > cap(rdbuf)
		rdbuf := make([]byte, pointer.length)
		if _, err := src.ReadAt(rdbuf, pointer.offset); err != nil {
			return err
		}
		if idx > 0 {
			if _, err := f.Write([]byte(",")); err != nil {
				return err
			}
		}
		msg.Reset()
		if err := proto.Unmarshal(rdbuf, &msg); err != nil {
			return err
		}
		if msg.Type != sourcebackendpb.SearchReply_MATCH {
			return fmt.Errorf("Expected to find a sourcebackendpb.SearchReply_MATCH, instead got %d", msg.Type)
		}
		match := msg.Match
		// We need to fix the ranking here because we persist raw results from
		// the dcs-source-backend in queryBackend(), but then modify the
		// ranking in storeResult().
		match.Ranking = match.Pathrank + ((firstPathRank * 0.1) * match.Ranking)
		if err := WriteMatchJSON(match, f); err != nil {
			return err
		}
	}
	if _, err := f.Write([]byte("]\n")); err != nil {
		return err
	}
	return nil
}

func (o *Opts) writeToDisk(s *queryState) error {
	// Get the slice with results and unset it on the state so that processing can continue.
	s.mu.Lock()
	numResults := s.numResults
	if numResults == 0 {
		log.Printf("[%s] not writing, no results.\n", s.queryid)
		s.mu.Unlock()
		return nil
	}
	idx := 0

	// For each full package (i3-wm_4.8-1), store only the newest version.
	packageVersions := make(map[string]version.Version)
	for _, bstate := range s.perBackend {
		for pkg, _ := range bstate.allPackages {
			underscore := strings.Index(pkg, "_")
			name := pkg[:underscore]
			ver, err := version.Parse(pkg[underscore+1:])
			if err != nil {
				log.Printf("[%s] parsing version %q failed: %v\n", s.queryid, pkg[underscore+1:], err)
				continue
			}

			if bestversion, ok := packageVersions[name]; ok {
				if version.Compare(ver, bestversion) > 0 {
					packageVersions[name] = ver
				}
			} else {
				packageVersions[name] = ver
			}
		}
	}

	packages := make([]string, len(packageVersions))
	for pkg, _ := range packageVersions {
		packages[idx] = pkg
		idx++
	}
	// TODO: sort by ranking as soon as we store the best ranking with each package. (at the moment it’s first result, first stored)
	s.allPackagesSorted = packages
	s.mu.Unlock()

	log.Printf("[%s] sorting, %d results, %d packages.\n", s.queryid, numResults, len(packages))
	pointerSortingStarted := time.Now()
	sorted, err := s.resultWriter.Flush()
	if err != nil {
		return err
	}
	log.Printf("[%s] pointer sorting done (%v).\n", s.queryid, time.Since(pointerSortingStarted))

	// TODO: it’d be so much better if we would correctly handle ESPACE errors
	// in the code below (and above), but for that we need to carefully test it.
	o.ensureEnoughSpaceAvailable()

	pages := int(math.Ceil(float64(numResults) / float64(resultsPerPage)))

	// Now save the results into their package-specific files.
	byPkgSortingStarted := time.Now()
	bypkg := make(map[string][]resultPointer)
	for pointer, err := range sorted.All() {
		if err != nil {
			return err
		}
		pkg := s.perBackend[pointer.backendidx].packagePool.Get(pointer.packageIdx)
		underscore := strings.Index(pkg, "_")
		name := pkg[:underscore]
		// Skip this result if it’s not in the newest version of the package.
		if packageVersions[name].String() != pkg[underscore+1:] {
			continue
		}
		pkgresults := bypkg[name]
		if len(pkgresults) >= resultsPerPackage {
			continue
		}
		pkgresults = append(pkgresults, pointer)
		bypkg[name] = pkgresults
	}
	log.Printf("[%s] by-pkg sorting done (%v).\n", s.queryid, time.Since(byPkgSortingStarted))

	s.mu.Lock()
	s.resultPointers = sorted
	s.resultPointersByPkg = bypkg
	s.resultPages = pages
	s.mu.Unlock()

	s.sendPaginationUpdate()
	return nil
}

func (o *Opts) storeProgress(s *queryState, backendidx int, progress *sourcebackendpb.ProgressUpdate) {
	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		log.Printf("query %s no longer exists", s.queryid)
		return
	}

	s.filesTotal[backendidx] = int(progress.FilesTotal)
	s.filesProcessed[backendidx] = int(progress.FilesProcessed)
	s.readBytes[backendidx] = progress.ReadBytes
	allSet := true
	for i := 0; i < len(o.backends); i++ {
		if s.filesTotal[i] == -1 {
			allSet = false
			break
		}
	}

	filesProcessed := 0
	for _, processed := range s.filesProcessed {
		filesProcessed += processed
	}
	filesTotal := 0
	for _, total := range s.filesTotal {
		filesTotal += total
	}
	var readBytes uint64
	for _, read := range s.readBytes {
		readBytes += read
	}
	numResults := s.numResults
	s.mu.Unlock()

	if !allSet {
		// progress reporting makes no sense yet:
		// the total number of work items (files) is not yet known.
		return
	}
	if filesProcessed == filesTotal {
		return // query already done
	}
	s.addEventMarshal(&ProgressUpdate{
		Type:           "progress",
		QueryId:        s.queryid,
		FilesProcessed: filesProcessed,
		FilesTotal:     filesTotal,
		Results:        numResults,
		ReadBytes:      readBytes,
	})
}

func (o *Opts) PerPackageResultsHandler(w http.ResponseWriter, r *http.Request) {
	matches := perPackagePathRe.FindStringSubmatch(r.URL.Path)
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
		http.Redirect(w, r, "/search?q="+matches[1]+pageSuffix+"&perpkg=1", http.StatusFound)
		return
	}

	queryid := matches[1]
	pagenr, err := strconv.Atoi(matches[2])
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not convert %q into a number: %v", matches[2], err), http.StatusBadRequest)
		return
	}
	s, ok := o.lookupQuery(queryid)
	if !ok {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "No such query.", http.StatusNotFound)
		return
	}
	select {
	case <-s.done:
	case <-time.After(60 * time.Second):
	case <-r.Context().Done():
	}
	if !s.completed() {
		log.Printf("[%s] query not yet finished, cannot produce per-package results\n", queryid)
		http.Error(w, "Query not finished yet.", http.StatusInternalServerError)
		return
	}

	// For compatibility with old versions, we serve the files that are
	// directly served by nginx as well by now.
	// This can be removed after 2015-06-01, when all old clients should be
	// long expired from any caches.
	name := filepath.Join(o.QueryResultsPath, queryid, fmt.Sprintf("perpackage_2_page_%d.json", pagenr))
	http.ServeFile(w, r, name)
}
