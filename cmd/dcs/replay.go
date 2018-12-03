package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp/syntax"
	"sort"
	"strings"
	"sync"
	"time"

	dcssearch "github.com/Debian/dcs/cmd/dcs-web/search"
	oldindex "github.com/Debian/dcs/index"
	"github.com/Debian/dcs/internal/index"
	"github.com/Debian/dcs/internal/sourcebackend"
	"github.com/Debian/dcs/ranking"
	"github.com/Debian/dcs/regexp"
)

const replayHelp = `replay - replay a query log

TODO

Example:
  % dcs replay -log=/home/michael/dcs-logs/2018-03-15/one-query-per-line.txt

`

type measurement struct {
	Index         int    `json:"index"`
	Query         string `json:"query"`
	FilesSearched int    `json:"files_searched"`
	PostingNano   int64  `json:"posting_nano"`
	Matches       int    `json:"matches"`
	QueryPos      bool   `json:"query_pos"`
	TotalNano     int64  `json:"total_nano"`
}

type shardedOldIndex struct {
	shards []*oldindex.Index
}

func (si *shardedOldIndex) doPostingQuery(query *oldindex.Query) []string {
	var (
		wg       sync.WaitGroup
		prefixed = make([][]string, len(si.shards))
	)
	for i := range si.shards {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			prefix := fmt.Sprintf("/srv/dcs-benchmark/shard%d/", i)
			ix := si.shards[i]
			post := ix.PostingQuery(query)
			possible := make([]string, len(post))
			for idx, fileid := range post {
				possible[idx] = prefix + ix.Name(fileid)
			}
			prefixed[i] = possible
		}(i)
	}
	wg.Wait()
	var possible []string
	for i := range si.shards {
		possible = append(possible, prefixed[i]...)
	}
	return possible
}

func (si *shardedOldIndex) measure(idx int, query string, _, skipFile, skipGrep bool) (measurement, error) {
	m := measurement{
		Index: idx,
		Query: query,
	}
	// Rewrite the query into a query for source backends.
	fakeUrl, err := url.Parse("?q=" + query)
	if err != nil {
		return m, err
	}
	rewritten := dcssearch.RewriteQuery(*fakeUrl)
	//log.Printf("rewritten: %q, query = %v", rewritten.String(), rewritten.Query())

	// Parse the (rewritten) URL to extract all ranking options/keywords.
	rankingopts := ranking.RankingOptsFromQuery(rewritten.Query())
	//log.Printf("rankingopts: %+v", rankingopts)

	// query all index files
	re, err := regexp.Compile(rewritten.Query().Get("q"))
	if err != nil {
		return m, fmt.Errorf("regexp.Compile: %s\n", err)
	}
	// if s := re.Syntax.Simplify(); s.Op == syntax.OpLiteral {
	// 	return m, fmt.Errorf("positional skipped")
	// }
	start := time.Now()
	possible := si.doPostingQuery(oldindex.RegexpQuery(re.Syntax))

	// Rank all the paths.
	files := make(ranking.ResultPaths, 0, len(possible))
	for _, filename := range possible {
		result := ranking.ResultPath{Path: filename}
		result.Rank(&rankingopts)
		if result.Ranking > -1 {
			files = append(files, result)
		}
	}
	files = sourcebackend.FilterByKeywords(&rewritten, files)
	m.FilesSearched = len(files)
	m.PostingNano = int64(time.Since(start))
	m.Matches = grep(rewritten.Query().Get("q"), files, rankingopts, skipFile, skipGrep)
	m.TotalNano = int64(time.Since(start))

	return m, nil
}

// TODO: refactor to verifyBundle(), use bcmills concurrency pattern
func verifyMatches(query string, files ranking.ResultPaths) (filesSearched int, matches int, _ error) {
	rqb := []byte(query)
	// matchFile, err := os.Create("/tmp/matchfile.pos.txt")
	// if err != nil {
	// 	return m, err
	// }
	// defer matchFile.Close()
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
				filesSearched++
			}
			bundle = append(bundle, fn)
		}
		if len(bundle) > 0 {
			work <- bundle
		}
		close(work)
	}()
	numWorkers := 1000
	if len(files) < numWorkers {
		numWorkers = len(files)
	}
	var matchesMu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			buf := make([]byte, 0, 64*1024)
			for bundle := range work {
				// Turns out open+read+close is significantly faster than
				// mmap'ing a whole bunch of small files (most of our files are
				// << 64 KB).
				// https://eklausmeier.wordpress.com/2016/02/03/performance-comparison-mmap-versus-read-versus-fread/
				f, err := os.Open(bundle[0].Path)
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
					if fn.Position+len(rqb) < len(b) && bytes.Equal(b[fn.Position:fn.Position+len(rqb)], rqb) {
						if lastPos > -1 && !bytes.ContainsRune(b[lastPos:fn.Position], '\n') {
							continue // cap to one match per line, like grep()
						}
						matchesMu.Lock()
						matches++
						matchesMu.Unlock()
						//fmt.Fprintf(matchFile, "%s:%d\n", fn.Path, fn.Position)
						lastPos = fn.Position
					}
				}
			}
		}()
	}
	wg.Wait()
	return filesSearched, matches, nil
}

func grep(query string, files ranking.ResultPaths, rankingopts ranking.RankingOpts, skipFile, skipGrep bool) int {
	// While not strictly necessary, this will lead to better results being
	// discovered (and returned!) earlier, so let’s spend a few cycles on
	// sorting the list of potential files first.
	sort.Sort(files)

	// The tricky part here is “flow control”: if we just start grepping like
	// crazy, we will eventually run out of memory because all our writes are
	// blocked on the connection (and the goroutines need to keep the write
	// buffer in memory until the write is done).
	//
	// So instead, we start 1000 worker goroutines and feed them work through a
	// single channel. Due to these goroutines being blocked on writing,
	// the grepping will naturally become slower.
	work := make(chan ranking.ResultPath)

	var wg sync.WaitGroup
	// TODO: add numWorkers && use defer, not files
	wg.Add(len(files))

	go func() {
		for _, file := range files {
			work <- file
		}
		close(work)
	}()

	querystr := ranking.NewQueryStr(query)

	// matchFile, err := os.Create("/tmp/matchfile.txt")
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// defer matchFile.Close()

	var (
		matchCntMu sync.Mutex
		matchCnt   int
		buf        = make([]byte, 0, 16384)
	)
	numWorkers := 1000
	if len(files) < 1000 {
		numWorkers = len(files)
	}
	for i := 0; i < numWorkers; i++ {
		go func() {
			re, err := regexp.Compile(query)
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
				if !skipFile {
					if skipGrep {
						if f, err := os.Open(file.Path); err == nil {
							if st, err := f.Stat(); err == nil {
								size := int(st.Size())
								if cap(buf) < size {
									size = cap(buf)
								}
								io.ReadFull(f, buf[:size])
							}
							f.Close()
						}
					} else {
						matches := grep.File(file.Path)
						matchCntMu.Lock()
						matchCnt += len(matches)
						// for _, match := range matches {
						// 	fmt.Fprintf(matchFile, "%s:%d\n", match.Path, match.Line)
						// }
						matchCntMu.Unlock()
					}
				}
				wg.Done()
			}
		}()
	}

	wg.Wait()

	return matchCnt
}

type shardedNewIndex struct {
	shards []*index.Index
}

func (si *shardedNewIndex) doPostingQuery(query *index.Query) []string {
	log.Printf("doPostingQuery(%s)", query)
	var (
		wg       sync.WaitGroup
		prefixed = make([][]string, len(si.shards))
	)
	for i := range si.shards {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ix := si.shards[i]
			post := ix.PostingQuery(query)
			possible := make([]string, len(post))
			for idx, fileid := range post {
				fn, err := ix.DocidMap.Lookup(fileid)
				if err != nil {
					log.Fatalf("DocidMap.Lookup(%v): %v", fileid, err)
				}
				possible[idx] = fn
			}
			prefixed[i] = possible
		}(i)
	}
	wg.Wait()
	l := 0
	for _, p := range prefixed {
		l += len(p)
	}
	possible := make([]string, 0, l)
	for i := range si.shards {
		possible = append(possible, prefixed[i]...)
	}
	return possible
}

type entry struct {
	fn  string
	pos uint32
}

func (si *shardedNewIndex) doPostingQueryPos(query string) []entry {
	log.Printf("doPostingQueryPos(%q)", query)
	var (
		wg       sync.WaitGroup
		prefixed = make([][]entry, len(si.shards))
	)
	for i := range si.shards {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ix := si.shards[i]
			matches, err := ix.QueryPositional(query)
			if err != nil {
				log.Fatalf("QueryPositional(%q): %v", query, err)
			}
			possible := make([]entry, len(matches))
			for idx, match := range matches {
				fn, err := ix.DocidMap.Lookup(match.Docid)
				if err != nil {
					log.Fatalf("DocidMap.Lookup(%v): %v", match.Docid, err)
				}
				possible[idx] = entry{
					fn:  fn,
					pos: match.Position,
				}
			}
			prefixed[i] = possible
		}(i)
	}
	wg.Wait()
	l := 0
	for _, p := range prefixed {
		l += len(p)
	}
	possible := make([]entry, 0, l)
	for i := range si.shards {
		possible = append(possible, prefixed[i]...)
	}
	return possible
}

func (si *shardedNewIndex) measure(idx int, query string, pos, skipFile, skipGrep bool) (measurement, error) {
	m := measurement{
		Index: idx,
		Query: query,
	}
	// Rewrite the query into a query for source backends.
	fakeUrl, err := url.Parse("?q=" + query)
	if err != nil {
		return m, err
	}
	rewritten := dcssearch.RewriteQuery(*fakeUrl)
	//log.Printf("rewritten: %q, query = %v", rewritten.String(), rewritten.Query())

	// Parse the (rewritten) URL to extract all ranking options/keywords.
	rankingopts := ranking.RankingOptsFromQuery(rewritten.Query())
	//log.Printf("rankingopts: %+v", rankingopts)

	// query all index files
	re, err := regexp.Compile(rewritten.Query().Get("q"))
	if err != nil {
		return m, fmt.Errorf("regexp.Compile: %s\n", err)
	}
	s := re.Syntax.Simplify()
	queryPos := pos && s.Op == syntax.OpLiteral
	m.QueryPos = queryPos
	start := time.Now()
	if queryPos {
		possible := si.doPostingQueryPos(string(s.Rune))
		files := make(ranking.ResultPaths, 0, len(possible))
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
		files = sourcebackend.FilterByKeywords(&rewritten, files)
		m.PostingNano = int64(time.Since(start))
		if !skipFile {
			filesSearched, matches, err := verifyMatches(string(s.Rune), files)
			if err != nil {
				return m, err
			}
			m.FilesSearched = filesSearched
			m.Matches = matches
		}
		m.TotalNano = int64(time.Since(start))
	} else {
		possible := si.doPostingQuery(index.RegexpQuery(re.Syntax))

		// Rank all the paths.
		files := make(ranking.ResultPaths, 0, len(possible))
		for _, filename := range possible {
			result := ranking.ResultPath{Path: filename}
			result.Rank(&rankingopts)
			if result.Ranking > -1 {
				files = append(files, result)
			}
		}
		files = sourcebackend.FilterByKeywords(&rewritten, files)
		m.FilesSearched = len(files)
		m.PostingNano = int64(time.Since(start))
		m.Matches = grep(rewritten.Query().Get("q"), files, rankingopts, skipFile, skipGrep)
		m.TotalNano = int64(time.Since(start))
	}
	return m, nil
}

type measurer interface {
	measure(idx int, query string, pos, skipFile, skipGrep bool) (measurement, error)
}

func logic(logPath string, old, pos bool, debug int, skipFile, skipGrep bool) error {
	var measurer measurer
	if old {
		si := &shardedOldIndex{}
		const shards = 6
		for i := 0; i < shards; i++ {
			ix := oldindex.Open(filepath.Join(fmt.Sprintf("/srv/dcs-benchmark/shard%d/", i), "full.idx"))
			defer ix.Close()
			si.shards = append(si.shards, ix)
		}
		measurer = si
	} else {
		si := &shardedNewIndex{}
		const shards = 6
		for i := 0; i < shards; i++ {
			ix, err := index.Open(fmt.Sprintf("/home/michael/as/shard%d/", i))
			if err != nil {
				return err
			}
			defer ix.Close()
			si.shards = append(si.shards, ix)
		}
		measurer = si
	}
	b, err := ioutil.ReadFile(logPath)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	for idx, query := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if debug > -1 && idx != debug {
			continue
		}

		log.Printf("query: %s", query)
		m, err := measurer.measure(idx, query, pos, skipFile, skipGrep)
		if err != nil {
			log.Printf("query %q failed: %v", query, err)
			continue
		}
		if err := enc.Encode(&m); err != nil {
			return err
		}
	}
	return nil

}

func replay(args []string) error {
	fset := flag.NewFlagSet("replay", flag.ExitOnError)
	var (
		logPath  = fset.String("log", "", "path to the query log file to replay (1 query per line)")
		old      = fset.Bool("old", false, "use the old index")
		pos      = fset.Bool("pos", false, "use the pos index")
		debug    = fset.Int("debug", -1, "if not -1, query index of the query to debug")
		skipFile = fset.Bool("skip_file", false, "index only")
		skipGrep = fset.Bool("skip_matching", false, "index + i/o")
	)
	if err := fset.Parse(args); err != nil {
		return err
	}
	if *logPath == "" {
		fset.Usage()
		os.Exit(1)
	}

	return logic(*logPath, *old, *pos, *debug, *skipFile, *skipGrep)
}
