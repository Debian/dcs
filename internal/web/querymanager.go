package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"log"
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
	"syscall"
	"time"
	"unsafe"

	"github.com/Debian/dcs/internal/frequency"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
	"github.com/Debian/dcs/internal/stringpool"
	"github.com/Debian/dcs/internal/web/common"
	"github.com/Debian/dcs/internal/web/search"
	"github.com/prometheus/client_golang/prometheus"
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
}

func (p *ProgressUpdate) EventType() string {
	return p.Type
}

func (p *ProgressUpdate) ObsoletedBy(newEvent *obsoletableEvent) bool {
	return (*newEvent).EventType() == p.Type
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
}

type queryState struct {
	queryid  string
	started  time.Time
	ended    time.Time
	events   []event
	newEvent *sync.Cond
	done     bool
	released bool // evicted (what a failed state map lookup used to mean)
	query    string

	results [10]resultPointer

	filesTotal     []int
	filesProcessed []int

	resultPages int

	// This guards concurrent access to any perBackend[].tempFile.
	tempFilesMu sync.Mutex
	perBackend  []*perBackendState

	numResults          int
	resultWriter        diskWriter
	resultPointers      sortedPointers
	resultPointersByPkg map[string][]resultPointer

	allPackagesSorted []string

	FirstPathRank float32
}

var (
	stateMu sync.RWMutex
	state   = make(map[string]*queryState)
)

func (o *Opts) queryBackend(ctx context.Context, queryid, src string, backend sourcebackendpb.SourceBackendClient, backendidx int, searchRequest *sourcebackendpb.SearchRequest) {
	// When exiting this function, check that all results were processed. If
	// not, the backend query must have failed for some reason. Send a progress
	// update to prevent the query from running forever.
	defer func() {
		stateMu.RLock()
		s, ok := state[queryid]
		var filesTotal int
		var filesProcessed int
		if ok {
			filesTotal = s.filesTotal[backendidx]
			filesProcessed = s.filesProcessed[backendidx]
		}
		stateMu.RUnlock()
		if !ok {
			return // query no longer exists
		}

		if filesProcessed == filesTotal {
			return
		}

		if filesTotal == -1 {
			filesTotal = 0
		}

		s.perBackend[backendidx].tempFileWriter.Flush()
		o.storeProgress(queryid, backendidx, &sourcebackendpb.ProgressUpdate{
			FilesProcessed: uint64(filesTotal),
			FilesTotal:     uint64(filesTotal),
		})

		s.addEventMarshal(&Error{
			Type:      "error",
			ErrorType: "backendunavailable",
		})
	}()

	ctx, cancelfunc := context.WithCancel(ctx)
	defer cancelfunc()
	stream, err := backend.Search(ctx, searchRequest)
	if err != nil {
		log.Printf("[%s] [src:%s] Search RPC failed: %v\n", queryid, src, err)
		return
	}

	stateMu.RLock()
	s, ok := state[queryid]
	stateMu.RUnlock()
	if !ok {
		log.Printf("[%s] [src:%s] query no longer exists\n", queryid, src)
		return
	}
	bstate := s.perBackend[backendidx]
	tempFileWriter := bstate.tempFileWriter
	orderlyFinished := false
	done := false

	for !done {
		msg, err := stream.Recv()
		if err == io.EOF {
			log.Printf("[%s] [src:%s] EOF\n", queryid, src)
			return
		}
		if err != nil {
			log.Printf("[%s] [src:%s] Error decoding result stream: %v\n", queryid, src, err)
			return
		}

		b, err := proto.Marshal(msg)
		if err != nil {
			log.Printf("[%s] [src:%s] Error encoding proto: %v\n", queryid, src, err)
			return
		}
		if _, err := tempFileWriter.Write(b); err != nil {
			log.Printf("[%s] [src:%s] Error writing proto: %v\n", queryid, src, err)
			return
		}

		switch msg.Type {
		case sourcebackendpb.SearchReply_MATCH:
			if err := storeResult(queryid, backendidx, msg.Match, len(b)); err != nil {
				log.Printf("[%s] [src:%s] Error writing result: %v\n", queryid, src, err)
				return
			}
		case sourcebackendpb.SearchReply_PROGRESS_UPDATE:
			orderlyFinished = msg.ProgressUpdate.FilesProcessed == msg.ProgressUpdate.FilesTotal
			if orderlyFinished {
				// Flush before storeProgress so that the file is on disk
				// before clients start requesting it.
				tempFileWriter.Flush()
			}
			o.storeProgress(queryid, backendidx, msg.ProgressUpdate)
		}

		bstate.tempFileOffset += int64(len(b))
		stateMu.RLock()
		s, ok := state[queryid]
		if ok {
			done = s.done
		}
		stateMu.RUnlock()
	}

	// Drain the stream: the above loop might finish early (when the query is cancelled)
	if orderlyFinished {
		// We got everything we need, but we need to try receiving one more
		// message to make gRPC realize the streaming RPC is finished (by
		// reading an EOF).
		stream.Recv()
	} else {
		// The query was cancelled before it could complete, so cancel the
		// stream as well.
		cancelfunc()
	}
	log.Printf("[%s] [src:%s] query done, disconnecting\n", queryid, src)
}

func (s *queryState) expired() bool {
	return time.Since(s.started) > 30*time.Minute
}

func releaseQueryLocked(s *queryState) {
	s.released = true
	for _, state := range s.perBackend {
		state.tempFile.Close()
	}
	s.newEvent.Broadcast() // unblock getEvent
}

func lookupQuery(queryid string) (*queryState, bool) {
	stateMu.RLock()
	defer stateMu.RUnlock()
	s, ok := state[queryid]
	return s, ok
}

func startQuery(querystate *queryState) (*queryState, bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	if s, ok := state[querystate.queryid]; ok {
		if !s.expired() {
			// This query is already active, do not interfere.
			return s, true
		}
		// Prepare for reusing the query slot by releasing resources.
		releaseQueryLocked(s)
	} else {
		// See if we need to garbage collect old queries. This is unnecessary when
		// the query is expired, as we can just re-use the previous slot.
		if len(state) >= 10 {
			log.Printf("Trying to garbage collect queries (currently %d)\n", len(state))
			for queryid, s := range state {
				if len(state) < 10 {
					break
				}
				if !s.done {
					continue
				}
				releaseQueryLocked(s)
				delete(state, queryid)
			}
			log.Printf("Garbage collection done. %d queries remaining", len(state))
		}
	}
	state[querystate.queryid] = querystate
	activeQueries.Add(1)
	frequency.IncUsers()
	return querystate, false
}

// XXX: Starting a new query while there may still be clients reading that
// query is not a great idea. Best fix may be to make getEvent() use a
// querystate instead of the string identifier.

// maybeStartQuery starts a specified query if that query does not already
// exist. Returns the *queryState and whether the query existed.
func (o *Opts) maybeStartQuery(ctx context.Context, queryid, src, query string) (*queryState, bool, error) {
	if s, ok := lookupQuery(queryid); ok && !s.expired() {
		return s, true, nil
	}

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
		query:          query,
		newEvent:       sync.NewCond(&stateMu),
		filesTotal:     make([]int, len(common.SourceBackendStubs)),
		filesProcessed: make([]int, len(common.SourceBackendStubs)),
		perBackend:     make([]*perBackendState, len(common.SourceBackendStubs)),
		resultPointers: memPointers(nil),
		resultWriter: diskWriter{
			// NOTE: append overshoots by up to 25%, so the 64 MB here
			// will be exceeded by up to 25% in practice.
			flushThreshold: int(64 * 1024 * 1024 / unsafe.Sizeof(resultPointer{})),
			dir:            dir,
		},
	}

	for i := 0; i < len(common.SourceBackendStubs); i++ {
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
	log.Printf("querystate = %v\n", querystate)

	// Rewrite the query into a query for source backends.
	fakeUrl, err := url.Parse("?" + query)
	if err != nil {
		log.Fatal(err)
	}
	rewritten := search.RewriteQuery(*fakeUrl)
	searchRequest := &sourcebackendpb.SearchRequest{
		Query:        rewritten.Query().Get("q"),
		RewrittenUrl: rewritten.String(),
		Literal:      rewritten.Query().Get("literal") == "1",
	}
	log.Printf("[%s] querying for %+v\n", queryid, searchRequest)
	if existing, ok := startQuery(querystate); ok {
		// Another goroutine must have raced us since we called lookupQuery().
		return existing, true, nil
	}
	for idx, backend := range common.SourceBackendStubs {
		go o.queryBackend(ctx, queryid, src, backend, idx, searchRequest)
	}
	return querystate, false, nil
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

func QueryzHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	if cancel := r.PostFormValue("cancel"); cancel != "" {
		if s, ok := lookupQuery(cancel); ok {
			s.addEventMarshal(&Error{
				Type:      "error",
				ErrorType: "cancelled",
			})
			finishQuery(cancel)
		}
		http.Redirect(w, r, "/queryz", http.StatusFound)
		return
	}

	stateMu.RLock()
	stats := make([]queryStats, len(state))
	idx := 0
	for queryid, s := range state {
		stats[idx] = queryStats{
			Searchterm:     s.query,
			QueryId:        queryid,
			NumEvents:      len(s.events),
			Done:           s.done,
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
		idx++
	}
	stateMu.RUnlock()

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
func sendPaginationUpdate(queryid string, s *queryState) {
	type Pagination struct {
		// Set to “pagination”.
		Type        string
		QueryId     string
		ResultPages int
	}

	if s.resultPages > 0 {
		s.addEventMarshal(&Pagination{
			Type:        "pagination",
			QueryId:     queryid,
			ResultPages: s.resultPages,
		})
	}
}

func storeResult(queryid string, backendidx int, result *sourcebackendpb.Match, resultLen int) error {
	h := fnv.New64()
	io.WriteString(h, result.Path)

	// Check if we need to consider this result for the top 10.
	stateMu.Lock()
	defer stateMu.Unlock()
	var firstPathRank float32
	s, ok := state[queryid]
	if !ok {
		return fmt.Errorf("query %s not found", queryid)
	}
	firstPathRank = s.FirstPathRank

	if firstPathRank > 0 {
		// Now store the combined ranking of PathRanking (pre) and Ranking (post).
		// We add the values because they are both percentages.
		// To make the Ranking (post) less significant, we multiply it with
		// 1/10 * FirstPathRank. We used to use maxPathRanking here, but
		// requiring that means delaying the search until all results are
		// there. Instead, FirstPathRank is a good enough approximation (but
		// different enough for each query that we can’t hardcode it).
		result.Ranking = result.Pathrank + ((firstPathRank * 0.1) * result.Ranking)
	} else {
		// This code path (and lock acquisition) gets executed only on the
		// first result.
		s.FirstPathRank = result.Pathrank
	}

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
		stateMu.Unlock()

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

		stateMu.Lock()
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

func failQuery(queryid string) {
	failedQueries.Inc()
	s, ok := lookupQuery(queryid)
	if !ok {
		return
	}
	s.addEventMarshal(&Error{
		Type:      "error",
		ErrorType: "failed",
	})
	finishQuery(queryid)
}

func finishQuery(queryid string) {
	stateMu.RLock()
	s, ok := state[queryid]
	if !ok {
		stateMu.RUnlock()
		return
	}
	started := s.started
	stateMu.RUnlock()
	log.Printf("[%s] done (in %v), closing all client channels.\n", queryid, time.Since(started))
	s.addEvent([]byte{}, nil)

	queryDurations.Observe(float64(time.Since(started) / time.Millisecond))
}

func fsBytes(path string) (available uint64, total uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		log.Fatalf("Could not stat filesystem for %q: %v\n", path, err)
	}
	log.Printf("Available bytes on %q: %d\n", path, stat.Bavail*uint64(stat.Bsize))
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

func writeFromPointers(queryid string, f io.Writer, pointers []resultPointer) error {
	stateMu.RLock()
	s, ok := state[queryid]
	var firstPathRank float32
	if ok {
		firstPathRank = s.FirstPathRank
	}
	stateMu.RUnlock()
	if !ok {
		return fmt.Errorf("query no longer exists")
	}

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

func (o *Opts) writeToDisk(queryid string) error {
	// Get the slice with results and unset it on the state so that processing can continue.
	stateMu.Lock()
	s, ok := state[queryid]
	if !ok {
		stateMu.Unlock()
		return fmt.Errorf("query no longer exists")
	}
	numResults := s.numResults
	if numResults == 0 {
		log.Printf("[%s] not writing, no results.\n", queryid)
		stateMu.Unlock()
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
				log.Printf("[%s] parsing version %q failed: %v\n", queryid, pkg[underscore+1:], err)
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
	state[queryid] = s
	stateMu.Unlock()

	log.Printf("[%s] sorting, %d results, %d packages.\n", queryid, numResults, len(packages))
	pointerSortingStarted := time.Now()
	sorted, err := s.resultWriter.Flush()
	if err != nil {
		return err
	}
	log.Printf("[%s] pointer sorting done (%v).\n", queryid, time.Since(pointerSortingStarted))

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
	log.Printf("[%s] by-pkg sorting done (%v).\n", queryid, time.Since(byPkgSortingStarted))

	stateMu.Lock()
	s, ok = state[queryid]
	if !ok {
		stateMu.Unlock()
		return fmt.Errorf("query no longer exists")
	}
	s.resultPointers = sorted
	s.resultPointersByPkg = bypkg
	s.resultPages = pages
	state[queryid] = s
	stateMu.Unlock()

	sendPaginationUpdate(queryid, s)
	return nil
}

func (o *Opts) storeProgress(queryid string, backendidx int, progress *sourcebackendpb.ProgressUpdate) {
	stateMu.Lock()
	s, ok := state[queryid]
	if !ok {
		stateMu.Unlock()
		return // query no longer exists
	}
	s.filesTotal[backendidx] = int(progress.FilesTotal)
	s.filesProcessed[backendidx] = int(progress.FilesProcessed)
	allSet := true
	for i := 0; i < len(common.SourceBackendStubs); i++ {
		if s.filesTotal[i] == -1 {
			log.Printf("total number for backend %d missing\n", i)
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
	numResults := s.numResults
	stateMu.Unlock()

	if allSet && filesProcessed == filesTotal {
		log.Printf("[%s] [src:%d] query done on all backends, writing to disk.\n", queryid, backendidx)
		if err := o.writeToDisk(queryid); err != nil {
			log.Printf("[%s] writeToDisk() failed: %v\n", queryid, err)
			failQuery(queryid)
		}
	}

	if allSet {
		log.Printf("[%s] [src:%d] (sending) progress: %d of %d\n", queryid, backendidx, progress.FilesProcessed, progress.FilesTotal)
		s.addEventMarshal(&ProgressUpdate{
			Type:           "progress",
			QueryId:        queryid,
			FilesProcessed: filesProcessed,
			FilesTotal:     filesTotal,
			Results:        numResults,
		})
		if filesProcessed == filesTotal {
			finishQuery(queryid)
		}
	} else {
		log.Printf("[%s] [src:%d] progress: %d of %d\n", queryid, backendidx, progress.FilesProcessed, progress.FilesTotal)
	}
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
	stateMu.RLock()
	s, ok := state[queryid]
	var done bool
	if ok {
		done = s.done
	}
	stateMu.RUnlock()
	if !ok {
		http.Error(w, "No such query.", http.StatusNotFound)
		return
	}
	if !done {
		started := time.Now()
		for time.Since(started) < 60*time.Second {
			stateMu.RLock()
			s, ok = state[queryid]
			if !ok {
				stateMu.RUnlock()
				log.Printf("[%s] query no longer exists\n", queryid)
				http.Error(w, "Query no longer exists.", http.StatusInternalServerError)
				return
			}
			if s.done {
				stateMu.RUnlock()
				break
			}
			stateMu.RUnlock()
			time.Sleep(100 * time.Millisecond)
		}
		stateMu.RLock()
		s, ok := state[queryid]
		done := ok && s.done
		stateMu.RUnlock()
		if !done {
			log.Printf("[%s] query not yet finished, cannot produce per-package results\n", queryid)
			http.Error(w, "Query not finished yet.", http.StatusInternalServerError)
			return
		}
	}

	// For compatibility with old versions, we serve the files that are
	// directly served by nginx as well by now.
	// This can be removed after 2015-06-01, when all old clients should be
	// long expired from any caches.
	name := filepath.Join(o.QueryResultsPath, queryid, fmt.Sprintf("perpackage_2_page_%d.json", pagenr))
	http.ServeFile(w, r, name)
}
