// vim:ts=4:sw=4:noexpandtab
package main

import (
	"bytes"
	"flag"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp/syntax"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Debian/dcs/grpcutil"
	"github.com/Debian/dcs/internal/index"
	"github.com/Debian/dcs/internal/proto/indexbackendpb"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
	"github.com/Debian/dcs/internal/sourcebackend"
	"github.com/Debian/dcs/ranking"
	"github.com/Debian/dcs/regexp"
	_ "github.com/Debian/dcs/varz"
	opentracing "github.com/opentracing/opentracing-go"
	olog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/uber/jaeger-client-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

var (
	listenAddress = flag.String("listen_address", ":28082", "listen address ([host]:port)")

	indexBackendAddr = flag.String("index_backend",
		"localhost:28081",
		"index backend host:port address")

	indexPath    = flag.String("index_path", "", "path to the index shard to serve, e.g. /dcs-ssd/index.0.idx")
	unpackedPath = flag.String("unpacked_path",
		"/dcs-ssd/unpacked/",
		"Path to the unpacked sources")
	rankingDataPath = flag.String("ranking_data_path",
		"/var/dcs/ranking.json",
		"Path to the JSON containing ranking data")
	tlsCertPath = flag.String("tls_cert_path", "", "Path to a .pem file containing the TLS certificate.")
	tlsKeyPath  = flag.String("tls_key_path", "", "Path to a .pem file containing the TLS private key.")
	jaegerAgent = flag.String("jaeger_agent",
		"localhost:5775",
		"host:port of a github.com/uber/jaeger agent")

	usePositionalIndex = flag.Bool("use_positional_index",
		false,
		"use the pos and posrel index sections for identifier queries")

	indexBackend indexbackendpb.IndexBackendClient
)

type SourceReply struct {
	// The number of the last used filename, needed for pagination
	LastUsedFilename int

	AllMatches []regexp.Match
}

type server struct {
	ix *index.Index
}

// Serves a single file for displaying it in /show
func (s *server) File(ctx context.Context, in *sourcebackendpb.FileRequest) (*sourcebackendpb.FileReply, error) {
	log.Printf("requested filename *%s*\n", in.Path)
	// path.Join calls path.Clean so we get the shortest path without any "..".
	absPath := path.Join(*unpackedPath, in.Path)
	log.Printf("clean, absolute path is *%s*\n", absPath)
	if !strings.HasPrefix(absPath, *unpackedPath) {
		return nil, fmt.Errorf("Path traversal is bad, mhkay?")
	}

	contents, err := ioutil.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	return &sourcebackendpb.FileReply{
		Contents: contents,
	}, nil
}

func sendProgressUpdate(stream sourcebackendpb.SourceBackend_SearchServer, connMu *sync.Mutex, filesProcessed, filesTotal int) error {
	connMu.Lock()
	defer connMu.Unlock()
	return stream.Send(&sourcebackendpb.SearchReply{
		Type: sourcebackendpb.SearchReply_PROGRESS_UPDATE,
		ProgressUpdate: &sourcebackendpb.ProgressUpdate{
			FilesProcessed: uint64(filesProcessed),
			FilesTotal:     uint64(filesTotal),
		},
	})
}

type entry struct {
	fn  string
	pos uint32
}

func countNL(b []byte) int {
	n := 0
	for {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			break
		}
		n++
		b = b[i+1:]
	}
	return n
}

func (s *server) queryPositional(literal string) ([]entry, error) {
	log.Printf("queryPositional(%q)", literal)
	matches, err := s.ix.QueryPositional(literal)
	if err != nil {
		return nil, fmt.Errorf("ix.QueryPositional(%q): %v", literal, err)
	}
	possible := make([]entry, len(matches))
	for idx, match := range matches {
		fn, err := s.ix.DocidMap.Lookup(match.Docid)
		if err != nil {
			return nil, fmt.Errorf("DocidMap.Lookup(%v): %v", match.Docid, err)
		}
		possible[idx] = entry{
			fn:  fn,
			pos: match.Position,
		}
	}

	return possible, nil
}

// Reads a single JSON request from the TCP connection, performs the search and
// sends results back over the TCP connection as they appear.
func (s *server) Search(in *sourcebackendpb.SearchRequest, stream sourcebackendpb.SourceBackend_SearchServer) error {
	ctx := stream.Context()
	connMu := new(sync.Mutex)
	logprefix := fmt.Sprintf("[%q]", in.Query)
	span := opentracing.SpanFromContext(ctx)

	re, err := regexp.Compile(in.Query)
	if err != nil {
		return fmt.Errorf("%s Could not compile regexp: %v\n", logprefix, err)
	}

	// Parse the (rewritten) URL to extract all ranking options/keywords.
	rewritten, err := url.Parse(in.RewrittenUrl)
	if err != nil {
		return err
	}
	rankingopts := ranking.RankingOptsFromQuery(rewritten.Query())
	span.LogFields(olog.String("rankingopts", fmt.Sprintf("%+v", rankingopts)))

	// TODO: analyze the query to see if fast path can be taken
	// maybe by using a different worker?
	simplified := re.Syntax.Simplify()
	queryPos := *usePositionalIndex && simplified.Op == syntax.OpLiteral
	var files ranking.ResultPaths
	if queryPos {
		possible, err := s.queryPositional(string(simplified.Rune))
		if err != nil {
			return err
		}
		files = make(ranking.ResultPaths, 0, len(possible))
		for _, entry := range possible {
			result := ranking.ResultPath{
				Path:     entry.fn,
				Position: int(entry.pos),
			}
			result.Rank(&rankingopts)
			if result.Ranking > -1 {
				files = append(files, result)
			}
		}
	} else {
		// Ask the local index backend for all the filenames.
		fstream, err := indexBackend.Files(ctx, &indexbackendpb.FilesRequest{Query: in.Query})
		if err != nil {
			return fmt.Errorf("%s Error querying index backend for query %q: %v\n", logprefix, in.Query, err)
		}

		var possible []string
		for {
			resp, err := fstream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			possible = append(possible, resp.Path)
		}

		span.LogFields(olog.Int("files.possible", len(possible)))

		// Rank all the paths.
		rankspan, _ := opentracing.StartSpanFromContext(ctx, "Rank")
		files = make(ranking.ResultPaths, 0, len(possible))
		for _, filename := range possible {
			result := ranking.ResultPath{Path: filename}
			result.Rank(&rankingopts)
			if result.Ranking > -1 {
				files = append(files, result)
			}
		}
		rankspan.Finish()
	}

	// Filter all files that should be excluded.
	filterspan, _ := opentracing.StartSpanFromContext(ctx, "Filter")
	files = sourcebackend.FilterByKeywords(rewritten, files)
	filterspan.Finish()

	span.LogFields(olog.Int("files.filtered", len(files)))

	// While not strictly necessary, this will lead to better results being
	// discovered (and returned!) earlier, so let’s spend a few cycles on
	// sorting the list of potential files first.
	if !queryPos {
		sort.Sort(files)
	}

	span.LogFields(olog.String("regexp", re.String()))

	log.Printf("%s regexp = %q, %d possible files\n", logprefix, re, len(files))

	// Send the first progress update so that clients know how many files are
	// going to be searched.
	if err := sendProgressUpdate(stream, connMu, 0, len(files)); err != nil {
		return fmt.Errorf("%s %v\n", logprefix, err)
	}

	// The tricky part here is “flow control”: if we just start grepping like
	// crazy, we will eventually run out of memory because all our writes are
	// blocked on the connection (and the goroutines need to keep the write
	// buffer in memory until the write is done).
	//
	// So instead, we start 1000 worker goroutines and feed them work through a
	// single channel. Due to these goroutines being blocked on writing,
	// the grepping will naturally become slower.
	progress := make(chan int)

	var wg sync.WaitGroup

	go func() {
		cnt := 0
		errorShown := false
		var lastProgressUpdate time.Time
		progressInterval := 2*time.Second + time.Duration(rand.Int63n(int64(500*time.Millisecond)))
		for cnt < len(files) {
			add := <-progress
			cnt += add

			if time.Since(lastProgressUpdate) > progressInterval {
				if err := sendProgressUpdate(stream, connMu, cnt, len(files)); err != nil {
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

		if err := sendProgressUpdate(stream, connMu, len(files), len(files)); err != nil {
			log.Printf("%s %v\n", logprefix, err)
		}
		close(progress)

		wg.Done()
	}()

	querystr := ranking.NewQueryStr(in.Query)

	numWorkers := 1000
	if len(files) < numWorkers {
		numWorkers = len(files)
	}
	var workerFn func()
	if queryPos {
		work := make(chan []ranking.ResultPath)
		go func() {
			var last string
			var bundle []ranking.ResultPath
			for _, fn := range files {
				if fn.Path != last {
					if len(bundle) > 0 {
						work <- bundle
						bundle = nil
					}
					last = fn.Path
				}
				bundle = append(bundle, fn)
			}
			if len(bundle) > 0 {
				work <- bundle
			}
			close(work)
		}()

		// We add the additional 1 for the progress updater goroutine. It also
		// needs to be done before we can return, otherwise it will try to use the
		// (already closed) network connection, which is a fatal error.
		wg.Add(numWorkers + 1)

		workerFn = func() {
			defer wg.Done()
			buf := make([]byte, 0, 64*1024)
			rqb := []byte(string(simplified.Rune))

			for bundle := range work {

				// TODO: figure out how to safely clone a dcs/regexp
				// Turns out open+read+close is significantly faster than
				// mmap'ing a whole bunch of small files (most of our files are
				// << 64 KB).
				// https://eklausmeier.wordpress.com/2016/02/03/performance-comparison-mmap-versus-read-versus-fread/
				f, err := os.Open(filepath.Join(*unpackedPath, bundle[0].Path))
				if err != nil {
					log.Fatal(err) // TODO
				}
				// Assumption: bundle is ordered from low to high (if not, we
				// need to traverse bundle).
				max := bundle[len(bundle)-1].Position + len(rqb)
				if max > cap(buf) {
					buf = make([]byte, 0, max)
				}
				n, err := f.Read(buf[:max])
				if err != nil {
					log.Fatal(err) // TODO
				}
				f.Close()
				if n != max {
					log.Fatalf("n = %d, max = %d\n", n, max)
				}
				b := buf[:n]

				lastPos := -1
				for _, fn := range bundle {
					progress <- 1
					sourcePkgName := fn.Path[fn.SourcePkgIdx[0]:fn.SourcePkgIdx[1]]
					if rankingopts.Pathmatch {
						fn.Ranking += querystr.Match(&fn.Path)
					}
					if rankingopts.Sourcepkgmatch {
						fn.Ranking += querystr.Match(&sourcePkgName)
					}
					if rankingopts.Weighted {
						fn.Ranking += 0.1460 * querystr.Match(&fn.Path)
						fn.Ranking += 0.0008 * querystr.Match(&sourcePkgName)
					}

					if fn.Position+len(rqb) >= len(b) || !bytes.Equal(b[fn.Position:fn.Position+len(rqb)], rqb) {
						continue
					}
					if lastPos > -1 && !bytes.ContainsRune(b[lastPos:fn.Position], '\n') {
						continue // cap to one match per line, like grep()
					}
					//fmt.Printf("%s:%d\n", fn.Path, fn.Position)
					lastPos = fn.Position

					line := countNL(b[:fn.Position]) + 1
					match := regexp.Match{
						Path: fn.Path,
						Line: line,
						//Context: string(line),
					}
					match.PathRank = ranking.PostRank(rankingopts, &match, &querystr)
					five := index.FiveLines(b, fn.Position)
					connMu.Lock()
					if err := stream.Send(&sourcebackendpb.SearchReply{
						Type: sourcebackendpb.SearchReply_MATCH,
						Match: &sourcebackendpb.Match{
							Path:     fn.Path,
							Line:     uint32(line),
							Package:  fn.Path[:strings.Index(fn.Path, "/")],
							Ctxp2:    html.EscapeString(five[0]),
							Ctxp1:    html.EscapeString(five[1]),
							Context:  html.EscapeString(five[2]),
							Ctxn1:    html.EscapeString(five[3]),
							Ctxn2:    html.EscapeString(five[4]),
							Pathrank: match.PathRank,
							Ranking:  fn.Ranking,
						},
					}); err != nil {
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
			}
		}
	} else {
		work := make(chan ranking.ResultPath)
		go func() {
			for _, file := range files {
				work <- file
			}
			close(work)
		}()

		// We add the additional 1 for the progress updater goroutine. It also
		// needs to be done before we can return, otherwise it will try to use the
		// (already closed) network connection, which is a fatal error.
		wg.Add(len(files) + 1)

		workerFn = func() {
			re, err := regexp.Compile(in.Query)
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

					// TODO: ideally, we’d get sourcebackendpb.Match structs from grep.File(), let’s do that after profiling the decoding performance

					path := match.Path[len(*unpackedPath):]
					connMu.Lock()
					if err := stream.Send(&sourcebackendpb.SearchReply{
						Type: sourcebackendpb.SearchReply_MATCH,
						Match: &sourcebackendpb.Match{
							Path:     path,
							Line:     uint32(match.Line),
							Package:  path[:strings.Index(path, "/")],
							Ctxp2:    match.Ctxp2,
							Ctxp1:    match.Ctxp1,
							Context:  match.Context,
							Ctxn1:    match.Ctxn1,
							Ctxn2:    match.Ctxn2,
							Pathrank: match.PathRank,
							Ranking:  match.Ranking,
						},
					}); err != nil {
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
		}
	}
	for i := 0; i < numWorkers; i++ {
		go workerFn()
	}

	wg.Wait()

	log.Printf("%s Sent all results.\n", logprefix)
	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()

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
	closer, err := cfg.InitGlobalTracer(
		"dcs-source-backend",
		jaegercfg.Logger(jaeger.StdLogger),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer closer.Close()

	rand.Seed(time.Now().UnixNano())
	if !strings.HasSuffix(*unpackedPath, "/") {
		*unpackedPath = *unpackedPath + "/"
	}
	fmt.Println("Debian Code Search source-backend")

	if err := ranking.ReadRankingData(*rankingDataPath); err != nil {
		log.Fatal(err)
	}

	conn, err := grpcutil.DialTLS(*indexBackendAddr, *tlsCertPath, *tlsKeyPath, grpc.WithBlock())
	if err != nil {
		log.Fatalf("could not connect to %q: %v", *indexBackendAddr, err)
	}
	defer conn.Close()
	indexBackend = indexbackendpb.NewIndexBackendClient(conn)

	idx := *indexPath
	if _, err := os.Stat(idx); os.IsNotExist(err) {
		tmp, err := ioutil.TempDir("", "dcs-index-backend")
		if err != nil {
			log.Fatal(err)
		}
		defer os.Remove(tmp)
		idx = tmp
		ix, err := index.Create(idx)
		if err != nil {
			log.Fatal(err)
		}
		if err := ix.Flush(); err != nil {
			log.Fatal(err)
		}
	}

	ix, err := index.Open(idx)
	if err != nil {
		log.Fatal(err)
	}

	srv := &server{ix: ix}

	http.Handle("/metrics", prometheus.Handler())
	log.Fatal(grpcutil.ListenAndServeTLS(*listenAddress,
		*tlsCertPath,
		*tlsKeyPath,
		func(s *grpc.Server) {
			sourcebackendpb.RegisterSourceBackendServer(s, srv)
		}))
}
