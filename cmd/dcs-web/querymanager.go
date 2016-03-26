package main

import (
	"bufio"
	"bytes"
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Debian/dcs/cmd/dcs-web/common"
	"github.com/Debian/dcs/cmd/dcs-web/search"
	"github.com/Debian/dcs/dpkgversion"
	pb "github.com/Debian/dcs/proto"
	"github.com/Debian/dcs/stringpool"
	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
)

var (
	queryResultsPath = flag.String("query_results_path",
		"/tmp/qr/",
		"Path where query results files (page_0.json etc.) are stored")

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

type ByRanking []pb.Match

func (s ByRanking) Len() int {
	return len(s)
}

func (s ByRanking) Less(i, j int) bool {
	if s[i].Ranking == s[j].Ranking {
		// On a tie, we use the path to make the order of results stable over
		// multiple queries (which can have different results depending on
		// which index backend reacts quicker).
		return s[i].Path > s[j].Path
	}
	return s[i].Ranking > s[j].Ranking
}

func (s ByRanking) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

type resultPointer struct {
	backendidx int
	ranking    float32
	offset     int64
	length     int

	// Used as a tie-breaker when sorting by ranking to guarantee stable
	// results, independent of the order in which the results are returned from
	// source backends.
	pathHash uint64

	// Used for per-package results. Points into a stringpool.StringPool
	packageName *string
}

type pointerByRanking []resultPointer

func (s pointerByRanking) Len() int {
	return len(s)
}

func (s pointerByRanking) Less(i, j int) bool {
	if s[i].ranking == s[j].ranking {
		return s[i].pathHash > s[j].pathHash
	}
	return s[i].ranking > s[j].ranking
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
	resultPointers []resultPointer
	allPackages    map[string]bool
}

type queryState struct {
	started  time.Time
	ended    time.Time
	events   []event
	newEvent *sync.Cond
	done     bool
	query    string

	results [10]resultPointer

	filesTotal     []int
	filesProcessed []int
	filesMu        *sync.Mutex

	resultPages int

	// This guards concurrent access to any perBackend[].tempFile.
	tempFilesMu *sync.Mutex
	perBackend  []*perBackendState

	resultPointers      []resultPointer
	resultPointersByPkg map[string][]resultPointer

	allPackagesSorted []string

	FirstPathRank float32
}

func (qs *queryState) numResults() int {
	var result int
	for _, bstate := range qs.perBackend {
		result += len(bstate.resultPointers)
	}
	return result
}

var (
	state   = make(map[string]queryState)
	stateMu sync.Mutex
)

func queryBackend(queryid string, backend pb.SourceBackendClient, backendidx int, searchRequest *pb.SearchRequest) {
	// When exiting this function, check that all results were processed. If
	// not, the backend query must have failed for some reason. Send a progress
	// update to prevent the query from running forever.
	defer func() {
		filesTotal := state[queryid].filesTotal[backendidx]

		if state[queryid].filesProcessed[backendidx] == filesTotal {
			return
		}

		if filesTotal == -1 {
			filesTotal = 0
		}

		storeProgress(queryid, backendidx, &pb.ProgressUpdate{
			FilesProcessed: uint64(filesTotal),
			FilesTotal:     uint64(filesTotal),
		})

		addEventMarshal(queryid, &Error{
			Type:      "error",
			ErrorType: "backendunavailable",
		})
	}()

	ctx, cancelfunc := context.WithCancel(context.Background())
	stream, err := backend.Search(ctx, searchRequest)
	if err != nil {
		log.Printf("[%s] [src:%s] Search RPC failed: %v\n", err)
		return
	}

	bstate := state[queryid].perBackend[backendidx]
	tempFileWriter := bstate.tempFileWriter
	buf := proto.NewBuffer(nil)
	orderlyFinished := false

	for !state[queryid].done {
		msg, err := stream.Recv()
		if err == io.EOF {
			log.Printf("[%s] [src:%s] EOF\n", queryid, backend)
			return
		}
		if err != nil {
			log.Printf("[%s] [src:%s] Error decoding result stream: %v\n", queryid, backend, err)
			return
		}

		buf.Reset()
		if err := buf.Marshal(msg); err != nil {
			log.Printf("[%s] [src:%s] Error encoding proto: %v\n", queryid, backend, err)
			return
		}
		if _, err := tempFileWriter.Write(buf.Bytes()); err != nil {
			log.Printf("[%s] [src:%s] Error writing proto: %v\n", queryid, backend, err)
			return
		}

		switch msg.Type {
		case pb.SearchReply_MATCH:
			storeResult(queryid, backendidx, msg.Match, len(buf.Bytes()))
		case pb.SearchReply_PROGRESS_UPDATE:
			storeProgress(queryid, backendidx, msg.ProgressUpdate)
			orderlyFinished = msg.ProgressUpdate.FilesProcessed == msg.ProgressUpdate.FilesTotal
		}

		bstate.tempFileOffset += int64(len(buf.Bytes()))
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
	log.Printf("[%s] [src:%s] query done, disconnecting\n", queryid, backend)
}

func maybeStartQuery(queryid, src, query string) bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	querystate, running := state[queryid]
	// XXX: Starting a new query while there may still be clients reading that
	// query is not a great idea. Best fix may be to make getEvent() use a
	// querystate instead of the string identifier.
	if !running || time.Since(querystate.started) > 30*time.Minute {
		// See if we can garbage-collect old queries.
		if !running && len(state) >= 10 {
			log.Printf("Trying to garbage collect queries (currently %d)\n", len(state))
			for queryid, s := range state {
				if len(state) < 10 {
					break
				}
				if !s.done {
					continue
				}
				for _, state := range s.perBackend {
					state.tempFile.Close()
				}
				delete(state, queryid)
			}
			log.Printf("Garbage collection done. %d queries remaining", len(state))
		}
		state[queryid] = queryState{
			started:        time.Now(),
			query:          query,
			newEvent:       sync.NewCond(&sync.Mutex{}),
			filesTotal:     make([]int, len(common.SourceBackendStubs)),
			filesProcessed: make([]int, len(common.SourceBackendStubs)),
			filesMu:        &sync.Mutex{},
			perBackend:     make([]*perBackendState, len(common.SourceBackendStubs)),
			tempFilesMu:    &sync.Mutex{},
		}

		activeQueries.Add(1)

		var err error
		dir := filepath.Join(*queryResultsPath, queryid)
		if err := os.MkdirAll(dir, os.FileMode(0755)); err != nil {
			log.Printf("[%s] could not create %q: %v\n", queryid, dir, err)
			failQuery(queryid)
			return false
		}

		// TODO: it’d be so much better if we would correctly handle ESPACE errors
		// in the code below (and above), but for that we need to carefully test it.
		ensureEnoughSpaceAvailable()

		for i := 0; i < len(common.SourceBackendStubs); i++ {
			state[queryid].filesTotal[i] = -1
			path := filepath.Join(dir, fmt.Sprintf("unsorted_%d.pb", i))
			f, err := os.Create(path)
			if err != nil {
				log.Printf("[%s] could not create %q: %v\n", queryid, path, err)
				failQuery(queryid)
				return false
			}
			state[queryid].perBackend[i] = &perBackendState{
				packagePool:    stringpool.NewStringPool(),
				tempFile:       f,
				tempFileWriter: bufio.NewWriterSize(f, 65536),
				allPackages:    make(map[string]bool),
			}
		}
		log.Printf("initial results = %v\n", state[queryid])

		// Rewrite the query into a query for source backends.
		fakeUrl, err := url.Parse("?" + query)
		if err != nil {
			log.Fatal(err)
		}
		rewritten := search.RewriteQuery(*fakeUrl)
		searchRequest := &pb.SearchRequest{
			Query:        rewritten.Query().Get("q"),
			RewrittenUrl: rewritten.String(),
		}
		log.Printf("[%s] querying for %+v\n", searchRequest)
		for idx, backend := range common.SourceBackendStubs {
			go queryBackend(queryid, backend, idx, searchRequest)
		}
		return false
	}

	return true
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

type byStarted []queryStats

func (s byStarted) Len() int {
	return len(s)
}

func (s byStarted) Less(i, j int) bool {
	return s[i].Started.After(s[j].Started)
}

func (s byStarted) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func QueryzHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	if cancel := r.PostFormValue("cancel"); cancel != "" {
		addEventMarshal(cancel, &Error{
			Type:      "error",
			ErrorType: "cancelled",
		})
		finishQuery(cancel)
		http.Redirect(w, r, "/queryz", http.StatusFound)
		return
	}

	stateMu.Lock()
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
			NumResults:     s.numResults(),
			NumResultPages: s.resultPages,
			FilesTotal:     s.filesTotal,
			FilesProcessed: s.filesProcessed,
		}
		if stats[idx].NumResults == 0 && stats[idx].Done {
			stats[idx].NumResults = s.numResults()
		}
		idx++
	}
	stateMu.Unlock()

	sort.Sort(byStarted(stats))

	if err := common.Templates.ExecuteTemplate(w, "queryz.html", map[string]interface{}{
		"queries": stats,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Caller needs to hold s.clientsMu
func sendPaginationUpdate(queryid string, s queryState) {
	type Pagination struct {
		// Set to “pagination”.
		Type        string
		QueryId     string
		ResultPages int
	}

	if s.resultPages > 0 {
		addEventMarshal(queryid, &Pagination{
			Type:        "pagination",
			QueryId:     queryid,
			ResultPages: s.resultPages,
		})
	}
}

func storeResult(queryid string, backendidx int, result *pb.Match, resultLen int) {
	// Without acquiring a lock, just check if we need to consider this result
	// for the top 10 at all.
	s := state[queryid]

	if s.FirstPathRank > 0 {
		// Now store the combined ranking of PathRanking (pre) and Ranking (post).
		// We add the values because they are both percentages.
		// To make the Ranking (post) less significant, we multiply it with
		// 1/10 * FirstPathRank. We used to use maxPathRanking here, but
		// requiring that means delaying the search until all results are
		// there. Instead, FirstPathRank is a good enough approximation (but
		// different enough for each query that we can’t hardcode it).
		result.Ranking = result.Pathrank + ((s.FirstPathRank * 0.1) * result.Ranking)
	} else {
		// This code path (and lock acquisition) gets executed only on the
		// first result.
		stateMu.Lock()
		s = state[queryid]
		s.FirstPathRank = result.Pathrank
		state[queryid] = s
		stateMu.Unlock()
	}

	h := fnv.New64()
	io.WriteString(h, result.Path)

	if result.Ranking > s.results[9].ranking {
		stateMu.Lock()
		s = state[queryid]
		if result.Ranking <= s.results[9].ranking {
			stateMu.Unlock()
		} else {
			// TODO: find the first s.result[] for the same package. then check again if the result is worthy of replacing that per-package result
			// TODO: probably change the data structure so that we can do this more easily and also keep N results per package.

			combined := append(s.results[:], resultPointer{
				ranking:  result.Ranking,
				pathHash: h.Sum64(),
			})
			sort.Sort(pointerByRanking(combined))
			copy(s.results[:], combined[:10])
			state[queryid] = s
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
			addEvent(queryid, b.Bytes(), &result)
		}
	}

	bstate := s.perBackend[backendidx]
	bstate.resultPointers = append(bstate.resultPointers, resultPointer{
		backendidx:  backendidx,
		ranking:     result.Ranking,
		offset:      bstate.tempFileOffset,
		length:      resultLen,
		pathHash:    h.Sum64(),
		packageName: bstate.packagePool.Get(result.Package)})
	bstate.allPackages[result.Package] = true
}

func failQuery(queryid string) {
	failedQueries.Inc()
	addEventMarshal(queryid, &Error{
		Type:      "error",
		ErrorType: "failed",
	})
	finishQuery(queryid)
}

func finishQuery(queryid string) {
	log.Printf("[%s] done (in %v), closing all client channels.\n", queryid, time.Since(state[queryid].started))
	addEvent(queryid, []byte{}, nil)

	queryDurations.Observe(float64(time.Since(state[queryid].started) / time.Millisecond))
}

type ByModTime []os.FileInfo

func (s ByModTime) Len() int {
	return len(s)
}

func (s ByModTime) Less(i, j int) bool {
	return s[i].ModTime().Before(s[j].ModTime())
}

func (s ByModTime) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func fsBytes(path string) (available uint64, total uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		log.Fatalf("Could not stat filesystem for %q: %v\n", path, err)
	}
	log.Printf("Available bytes on %q: %d\n", path, stat.Bavail*uint64(stat.Bsize))
	available = stat.Bavail * uint64(stat.Bsize)
	total = stat.Blocks * uint64(stat.Bsize)
	return
}

// Makes sure 20% of the filesystem backing -query_results_path are available,
// cleans up old query results otherwise.
func ensureEnoughSpaceAvailable() {
	available, total := fsBytes(*queryResultsPath)
	headroom := uint64(0.2 * float64(total))
	log.Printf("%d bytes available, %d bytes headroom required (20%%)\n", available, headroom)
	if available >= headroom {
		return
	}

	log.Printf("Deleting an old query...\n")
	dir, err := os.Open(*queryResultsPath)
	if err != nil {
		log.Fatal(err)
	}
	defer dir.Close()
	infos, err := dir.Readdir(-1)
	if err != nil {
		log.Fatal(err)
	}
	sort.Sort(ByModTime(infos))
	for _, info := range infos {
		if !info.IsDir() {
			continue
		}
		log.Printf("Removing query results for %q to make enough space\n", info.Name())
		if err := os.RemoveAll(filepath.Join(*queryResultsPath, info.Name())); err != nil {
			log.Fatal(err)
		}
		available, _ = fsBytes(*queryResultsPath)
		if available >= headroom {
			break
		}
	}
}

func writeFromPointers(queryid string, f io.Writer, pointers []resultPointer) error {
	firstPathRank := state[queryid].FirstPathRank

	state[queryid].tempFilesMu.Lock()
	defer state[queryid].tempFilesMu.Unlock()

	if _, err := f.Write([]byte("[")); err != nil {
		return err
	}
	var msg pb.SearchReply
	buf := proto.NewBuffer(nil)
	for idx, pointer := range pointers {
		src := state[queryid].perBackend[pointer.backendidx].tempFile
		if _, err := src.Seek(pointer.offset, os.SEEK_SET); err != nil {
			return err
		}
		// TODO: Avoid the allocations by using a slice and only allocate a new buffer when pointer.length > cap(rdbuf)
		rdbuf := make([]byte, pointer.length)
		if _, err := src.Read(rdbuf); err != nil {
			return err
		}
		if idx > 0 {
			if _, err := f.Write([]byte(",")); err != nil {
				return err
			}
		}
		buf.SetBuf(rdbuf)
		if err := buf.Unmarshal(&msg); err != nil {
			return err
		}
		if msg.Type != pb.SearchReply_MATCH {
			return fmt.Errorf("Expected to find a pb.SearchReply_MATCH, instead got %d", msg.Type)
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

func writeToDisk(queryid string) error {
	// Get the slice with results and unset it on the state so that processing can continue.
	stateMu.Lock()
	s := state[queryid]
	pointers := make([]resultPointer, 0, s.numResults())
	for _, bstate := range s.perBackend {
		pointers = append(pointers, bstate.resultPointers...)
		bstate.tempFileWriter.Flush()
	}
	if len(pointers) == 0 {
		log.Printf("[%s] not writing, no results.\n", queryid)
		stateMu.Unlock()
		return nil
	}
	idx := 0

	// For each full package (i3-wm_4.8-1), store only the newest version.
	packageVersions := make(map[string]dpkgversion.Version)
	for _, bstate := range s.perBackend {
		for pkg, _ := range bstate.allPackages {
			underscore := strings.Index(pkg, "_")
			name := pkg[:underscore]
			version, err := dpkgversion.Parse(pkg[underscore+1:])
			if err != nil {
				log.Printf("[%s] parsing version %q failed: %v\n", queryid, pkg[underscore+1:], err)
				continue
			}

			if bestversion, ok := packageVersions[name]; ok {
				if dpkgversion.Compare(version, bestversion) > 0 {
					packageVersions[name] = version
				}
			} else {
				packageVersions[name] = version
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

	log.Printf("[%s] sorting, %d results, %d packages.\n", queryid, len(pointers), len(packages))
	pointerSortingStarted := time.Now()
	sort.Sort(pointerByRanking(pointers))
	log.Printf("[%s] pointer sorting done (%v).\n", queryid, time.Since(pointerSortingStarted))

	// TODO: it’d be so much better if we would correctly handle ESPACE errors
	// in the code below (and above), but for that we need to carefully test it.
	ensureEnoughSpaceAvailable()

	pages := int(math.Ceil(float64(len(pointers)) / float64(resultsPerPage)))

	// Now save the results into their package-specific files.
	byPkgSortingStarted := time.Now()
	bypkg := make(map[string][]resultPointer)
	for _, pointer := range pointers {
		pkg := *pointer.packageName
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
	s = state[queryid]
	s.resultPointers = pointers
	s.resultPointersByPkg = bypkg
	s.resultPages = pages
	state[queryid] = s
	stateMu.Unlock()

	sendPaginationUpdate(queryid, s)
	return nil
}

func storeProgress(queryid string, backendidx int, progress *pb.ProgressUpdate) {
	s := state[queryid]
	s.filesMu.Lock()
	s.filesTotal[backendidx] = int(progress.FilesTotal)
	s.filesProcessed[backendidx] = int(progress.FilesProcessed)
	s.filesMu.Unlock()
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

	if allSet && filesProcessed == filesTotal {
		log.Printf("[%s] [src:%d] query done on all backends, writing to disk.\n", queryid, backendidx)
		if err := writeToDisk(queryid); err != nil {
			log.Printf("[%s] writeToDisk() failed: %v\n", queryid, err)
			failQuery(queryid)
		}
	}

	if allSet {
		log.Printf("[%s] [src:%d] (sending) progress: %d of %d\n", queryid, backendidx, progress.FilesProcessed, progress.FilesTotal)
		addEventMarshal(queryid, &ProgressUpdate{
			Type:           "progress",
			QueryId:        queryid,
			FilesProcessed: filesProcessed,
			FilesTotal:     filesTotal,
			Results:        s.numResults(),
		})
		if filesProcessed == filesTotal {
			finishQuery(queryid)
		}
	} else {
		log.Printf("[%s] [src:%d] progress: %d of %d\n", queryid, backendidx, progress.FilesProcessed, progress.FilesTotal)
	}
}

func PerPackageResultsHandler(w http.ResponseWriter, r *http.Request) {
	matches := perPackagePathRe.FindStringSubmatch(r.URL.Path)
	if matches == nil || len(matches) != 3 {
		// TODO: what about non-js clients?
		// While this just serves index.html, the javascript part of index.html
		// realizes the path starts with /perpackage-results/ and starts the
		// search, then requests the specified page on search completion.
		http.ServeFile(w, r, filepath.Join(*staticPath, "index.html"))
		return
	}

	queryid := matches[1]
	pagenr, err := strconv.Atoi(matches[2])
	if err != nil {
		log.Fatalf("Could not convert %q into a number: %v\n", matches[2], err)
	}
	s, ok := state[queryid]
	if !ok {
		http.Error(w, "No such query.", http.StatusNotFound)
		return
	}
	if !s.done {
		started := time.Now()
		for time.Since(started) < 60*time.Second {
			if state[queryid].done {
				s = state[queryid]
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !s.done {
			log.Printf("[%s] query not yet finished, cannot produce per-package results\n", queryid)
			http.Error(w, "Query not finished yet.", http.StatusInternalServerError)
			return
		}
	}

	// For compatibility with old versions, we serve the files that are
	// directly served by nginx as well by now.
	// This can be removed after 2015-06-01, when all old clients should be
	// long expired from any caches.
	name := filepath.Join(*queryResultsPath, queryid, fmt.Sprintf("perpackage_2_page_%d.json", pagenr))
	http.ServeFile(w, r, name)
}
