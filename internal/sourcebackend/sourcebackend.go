package sourcebackend

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io/ioutil"
	"log"
	"math/rand"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp/syntax"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Debian/dcs/internal/index"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
	"github.com/Debian/dcs/ranking"
	"github.com/Debian/dcs/regexp"
	"github.com/google/renameio"
	opentracing "github.com/opentracing/opentracing-go"
	olog "github.com/opentracing/opentracing-go/log"
)

func FilterByKeywords(rewritten *url.URL, files []ranking.ResultPath) []ranking.ResultPath {
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
		pkgRegexp, err := regexp.Compile(pkg)
		if err != nil {
			return files
		}
		filtered := make(ranking.ResultPaths, 0, len(files))
		for _, file := range files {
			if pkgRegexp.MatchString(file.Path[file.SourcePkgIdx[0]:file.SourcePkgIdx[1]], true, true) == -1 {
				continue
			}

			filtered = append(filtered, file)
		}

		files = filtered
	}

	// Filter the filenames if the "-package:" keyword was specified.
	for _, npkg := range npkgs {
		npkgRegexp, err := regexp.Compile(npkg)
		if err != nil {
			return files
		}
		filtered := make(ranking.ResultPaths, 0, len(files))
		for _, file := range files {
			if npkgRegexp.MatchString(file.Path[file.SourcePkgIdx[0]:file.SourcePkgIdx[1]], true, true) != -1 {
				continue
			}

			filtered = append(filtered, file)
		}

		files = filtered
	}

	for _, path := range paths {
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

type SourceReply struct {
	// The number of the last used filename, needed for pagination
	LastUsedFilename int

	AllMatches []regexp.Match
}

type Server struct {
	mu                 sync.Mutex
	Index              *index.Index
	UnpackedPath       string
	IndexPath          string
	UsePositionalIndex bool
}

// Serves a single file for displaying it in /show
func (s *Server) File(ctx context.Context, in *sourcebackendpb.FileRequest) (*sourcebackendpb.FileReply, error) {
	log.Printf("requested filename *%s*\n", in.Path)
	// path.Join calls path.Clean so we get the shortest path without any "..".
	absPath := path.Join(s.UnpackedPath, in.Path)
	log.Printf("clean, absolute path is *%s*\n", absPath)
	if !strings.HasPrefix(absPath, s.UnpackedPath) {
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

func (s *Server) ReplaceIndex(ctx context.Context, in *sourcebackendpb.ReplaceIndexRequest) (*sourcebackendpb.ReplaceIndexReply, error) {
	newShard := in.ReplacementPath

	file, err := os.Open(filepath.Dir(s.IndexPath))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	names, err := file.Readdirnames(-1)
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		if name == newShard {
			newShard = filepath.Join(filepath.Dir(s.IndexPath), name)
			// We verified the given argument refers to an index shard within
			// this directory, so let’s load this shard.
			oldIndex := s.Index
			log.Printf("Trying to load %q\n", newShard)
			newIndex, err := index.Open(newShard)
			if err != nil {
				return nil, err
			}
			s.mu.Lock()
			s.Index = newIndex
			s.mu.Unlock()
			defer oldIndex.Close()

			if err := renameio.Symlink(newShard, s.IndexPath); err != nil {
				return nil, err
			}
			fis, err := ioutil.ReadDir(filepath.Dir(s.IndexPath))
			if err != nil {
				return nil, err
			}
			for _, fi := range fis {
				if !strings.HasPrefix(fi.Name(), "full.") {
					continue
				}
				if fi.Name() == name {
					continue
				}
				log.Printf("Removing old index %q", fi.Name())
				if err := os.RemoveAll(filepath.Join(filepath.Dir(s.IndexPath), fi.Name())); err != nil {
					return nil, err
				}
			}
			return &sourcebackendpb.ReplaceIndexReply{}, nil
		}
	}

	return nil, fmt.Errorf("No such shard.")
}

func (s *Server) queryPositional(literal string) ([]entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	log.Printf("queryPositional(%q)", literal)
	matches, err := s.Index.QueryPositional(literal)
	if err != nil {
		return nil, fmt.Errorf("ix.QueryPositional(%q): %v", literal, err)
	}
	possible := make([]entry, len(matches))
	for idx, match := range matches {
		fn, err := s.Index.DocidMap.Lookup(match.Docid)
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

func (s *Server) query(query *index.Query) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	post := s.Index.PostingQuery(query)
	possible := make([]string, len(post))
	var err error
	for idx, docid := range post {
		possible[idx], err = s.Index.DocidMap.Lookup(docid)
		if err != nil {
			return nil, err
		}
	}
	return possible, nil
}

// Reads a single JSON request from the TCP connection, performs the search and
// sends results back over the TCP connection as they appear.
func (s *Server) Search(in *sourcebackendpb.SearchRequest, stream sourcebackendpb.SourceBackend_SearchServer) error {
	ctx := stream.Context()
	connMu := new(sync.Mutex)
	logprefix := fmt.Sprintf("[%q]", in.Query)
	span := opentracing.SpanFromContext(ctx)
	if span == nil {
		span = (&opentracing.NoopTracer{}).StartSpan("Search")
	}

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
	queryPos := s.UsePositionalIndex && simplified.Op == syntax.OpLiteral
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
		possible, err := s.query(index.RegexpQuery(re.Syntax))
		if err != nil {
			return err
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
	files = FilterByKeywords(rewritten, files)
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
				f, err := os.Open(filepath.Join(s.UnpackedPath, bundle[0].Path))
				if err != nil {
					log.Printf("%s %v", logprefix, err)
					for range bundle {
						progress <- 1
					}
					continue
				}
				const extraBytes = 1024 // for context lines
				// Assumption: bundle is ordered from low to high (if not, we
				// need to traverse bundle).
				max := bundle[len(bundle)-1].Position + len(rqb) + extraBytes
				if max > cap(buf) {
					buf = make([]byte, 0, max)
				}
				n, err := f.Read(buf[:max])
				if err != nil {
					log.Printf("%s %v", logprefix, err)
					for range bundle {
						progress <- 1
					}
					continue
				}
				f.Close()
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

					if fn.Position+len(rqb) > len(b) || !bytes.Equal(b[fn.Position:fn.Position+len(rqb)], rqb) {
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
				matches := grep.File(path.Join(s.UnpackedPath, file.Path))
				for _, match := range matches {
					match.Ranking = ranking.PostRank(rankingopts, &match, &querystr)
					match.PathRank = file.Ranking
					//match.Path = match.Path[len(*unpackedPath):]
					// NB: populating match.Ranking happens in
					// cmd/dcs-web/querymanager because it depends on at least
					// one other result.

					// TODO: ideally, we’d get sourcebackendpb.Match structs from grep.File(), let’s do that after profiling the decoding performance

					path := match.Path[len(s.UnpackedPath):]
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
