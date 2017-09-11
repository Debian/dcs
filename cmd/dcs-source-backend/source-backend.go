// vim:ts=4:sw=4:noexpandtab
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Debian/dcs/grpcutil"
	"github.com/Debian/dcs/proto"
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
	unpackedPath  = flag.String("unpacked_path",
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

	indexBackend proto.IndexBackendClient
)

type SourceReply struct {
	// The number of the last used filename, needed for pagination
	LastUsedFilename int

	AllMatches []regexp.Match
}

type server struct {
}

// Serves a single file for displaying it in /show
func (s *server) File(ctx context.Context, in *proto.FileRequest) (*proto.FileReply, error) {
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
	return &proto.FileReply{
		Contents: contents,
	}, nil
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

func sendProgressUpdate(stream proto.SourceBackend_SearchServer, connMu *sync.Mutex, filesProcessed, filesTotal int) error {
	connMu.Lock()
	defer connMu.Unlock()
	return stream.Send(&proto.SearchReply{
		Type: proto.SearchReply_PROGRESS_UPDATE,
		ProgressUpdate: &proto.ProgressUpdate{
			FilesProcessed: uint64(filesProcessed),
			FilesTotal:     uint64(filesTotal),
		},
	})
}

// Reads a single JSON request from the TCP connection, performs the search and
// sends results back over the TCP connection as they appear.
func (s *server) Search(in *proto.SearchRequest, stream proto.SourceBackend_SearchServer) error {
	ctx := stream.Context()
	connMu := new(sync.Mutex)
	logprefix := fmt.Sprintf("[%q]", in.Query)
	span := opentracing.SpanFromContext(ctx)

	// Ask the local index backend for all the filenames.
	resp, err := indexBackend.Files(ctx, &proto.FilesRequest{Query: in.Query})
	if err != nil {
		return fmt.Errorf("%s Error querying index backend for query %q: %v\n", logprefix, in.Query, err)
	}

	span.LogFields(olog.Int("files.possible", len(resp.Path)))

	// Parse the (rewritten) URL to extract all ranking options/keywords.
	rewritten, err := url.Parse(in.RewrittenUrl)
	if err != nil {
		return err
	}
	rankingopts := ranking.RankingOptsFromQuery(rewritten.Query())
	span.LogFields(olog.String("rankingopts", fmt.Sprintf("%+v", rankingopts)))

	// Rank all the paths.
	rankspan, _ := opentracing.StartSpanFromContext(ctx, "Rank")
	files := make(ranking.ResultPaths, 0, len(resp.Path))
	for _, filename := range resp.Path {
		result := ranking.ResultPath{Path: filename}
		result.Rank(&rankingopts)
		if result.Ranking > -1 {
			files = append(files, result)
		}
	}
	rankspan.Finish()

	// Filter all files that should be excluded.
	filterspan, _ := opentracing.StartSpanFromContext(ctx, "Filter")
	files = filterByKeywords(rewritten, files)
	filterspan.Finish()

	span.LogFields(olog.Int("files.filtered", len(files)))

	// While not strictly necessary, this will lead to better results being
	// discovered (and returned!) earlier, so let’s spend a few cycles on
	// sorting the list of potential files first.
	sort.Sort(files)

	re, err := regexp.Compile(in.Query)
	if err != nil {
		return fmt.Errorf("%s Could not compile regexp: %v\n", logprefix, err)
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
	if len(files) < 1000 {
		numWorkers = len(files)
	}
	for i := 0; i < numWorkers; i++ {
		go func() {
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

					// TODO: ideally, we’d get proto.Match structs from grep.File(), let’s do that after profiling the decoding performance

					path := match.Path[len(*unpackedPath):]
					connMu.Lock()
					if err := stream.Send(&proto.SearchReply{
						Type: proto.SearchReply_MATCH,
						Match: &proto.Match{
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
		}()
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

	conn, err := grpcutil.DialTLS("localhost:28081", *tlsCertPath, *tlsKeyPath)
	if err != nil {
		log.Fatalf("could not connect to %q: %v", "localhost:28081", err)
	}
	defer conn.Close()
	indexBackend = proto.NewIndexBackendClient(conn)

	http.Handle("/metrics", prometheus.Handler())
	log.Fatal(grpcutil.ListenAndServeTLS(*listenAddress,
		*tlsCertPath,
		*tlsKeyPath,
		func(s *grpc.Server) {
			proto.RegisterSourceBackendServer(s, &server{})
		}))
}
