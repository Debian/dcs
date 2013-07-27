// vim:ts=4:sw=4:noexpandtab
package main

import (
	"dcs/index"
	"code.google.com/p/codesearch/regexp"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"runtime/pprof"
	"time"
)

var ix *index.Index

var (
	listenAddress = flag.String("listen_address", ":28081", "listen address ([host]:port)")
	indexPath = flag.String("index_path", "", "path to the index shard to serve, e.g. /dcs-ssd/index.0.idx")
	cpuProfile = flag.String("cpuprofile", "", "write cpu profile to this file")
	id string
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
	post := ix.PostingQuery(query)
	t1 := time.Now()
	fmt.Printf("[%s] postingquery done in %v, %d results\n", id, t1.Sub(t0), len(post))
	files := make([]string, len(post))
	for idx, fileid := range post {
		files[idx] = ix.Name(fileid)
	}
	t2 := time.Now()
	fmt.Printf("[%s] filenames collected in %v\n", id, t2.Sub(t1))
	jsonFiles, err := json.Marshal(files)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
	t3 := time.Now()
	fmt.Printf("[%s] marshaling done in %v\n", id, t3.Sub(t2))
	_, err = w.Write(jsonFiles)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
	t4 := time.Now()
	fmt.Printf("[%s] written in %v\n", id, t4.Sub(t3))
}

func main() {
	flag.Parse()
	if *indexPath == "" {
		log.Fatal("You need to specify a non-empty -index_path")
	}
	fmt.Println("Debian Code Search index-backend")

	id = path.Base(*indexPath)

	ix = index.Open(*indexPath)

	http.HandleFunc("/index", Index)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
