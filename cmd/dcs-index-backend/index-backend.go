// vim:ts=4:sw=4:noexpandtab
package main

import (
	"code.google.com/p/codesearch/regexp"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Debian/dcs/index"
	"github.com/Debian/dcs/varz"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"time"
)

var (
	listenAddress = flag.String("listen_address", ":28081", "listen address ([host]:port)")
	indexPath     = flag.String("index_path", "", "path to the index shard to serve, e.g. /dcs-ssd/index.0.idx")
	cpuProfile    = flag.String("cpuprofile", "", "write cpu profile to this file")

	id      string
	ix      *index.Index
	ixMutex sync.Mutex
)

// Handles requests to /index by compiling the q= parameter into a regular
// expression (codesearch/regexp), searching the index for it and returning the
// list of matching filenames in a JSON array.
// TODO: This doesn’t handle file name regular expressions at all yet.
// TODO: errors aren’t properly signaled to the requester
func Index(w http.ResponseWriter, r *http.Request) {
	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	r.ParseForm()
	textQuery := r.Form.Get("q")
	re, err := regexp.Compile(textQuery)
	if err != nil {
		log.Printf("regexp.Compile: %s\n", err)
		return
	}
	query := index.RegexpQuery(re.Syntax)
	log.Printf("[%s] query: text = %s, regexp = %s\n", id, textQuery, query)
	t0 := time.Now()
	ixMutex.Lock()
	post := ix.PostingQuery(query)
	t1 := time.Now()
	fmt.Printf("[%s] postingquery done in %v, %d results\n", id, t1.Sub(t0), len(post))
	files := make([]string, len(post))
	for idx, fileid := range post {
		files[idx] = ix.Name(fileid)
	}
	ixMutex.Unlock()
	t2 := time.Now()
	fmt.Printf("[%s] filenames collected in %v\n", id, t2.Sub(t1))
	if err := json.NewEncoder(w).Encode(files); err != nil {
		log.Printf("%s\n", err)
		return
	}
	t3 := time.Now()
	fmt.Printf("[%s] written in %v\n", id, t3.Sub(t2))
}

func Replace(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	newShard := r.Form.Get("shard")

	file, err := os.Open(filepath.Dir(*indexPath))
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	names, err := file.Readdirnames(-1)
	if err != nil {
		log.Fatal(err)
	}

	for _, name := range names {
		if name == newShard {
			newShard = filepath.Join(filepath.Dir(*indexPath), name)
			// We verified the given argument refers to an index shard within
			// this directory, so let’s load this shard.
			oldIndex := ix
			log.Printf("Trying to load %q\n", newShard)
			ixMutex.Lock()
			ix = index.Open(newShard)
			ixMutex.Unlock()
			// Overwrite the old full shard with the new one. This is necessary
			// so that the state is persistent across restarts and has the nice
			// side-effect of cleaning up the old full shard.
			if err := os.Rename(newShard, *indexPath); err != nil {
				log.Fatal(err)
			}
			oldIndex.Close()
			return
		}
	}

	http.Error(w, "No such shard.", http.StatusInternalServerError)
}

func main() {
	flag.Parse()
	if *indexPath == "" {
		log.Fatal("You need to specify a non-empty -index_path")
	}
	fmt.Println("Debian Code Search index-backend")

	id = filepath.Base(*indexPath)
	ix = index.Open(*indexPath)

	http.HandleFunc("/index", Index)
	http.HandleFunc("/replace", Replace)
	http.HandleFunc("/varz", varz.Varz)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
