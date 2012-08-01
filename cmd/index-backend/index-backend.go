// vim:ts=4:sw=4:noexpandtab
package main

import (
	"code.google.com/p/codesearch/index"
	"code.google.com/p/codesearch/regexp"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/pprof"
	"time"
)

var ix *index.Index

var (
	listenAddress = flag.String("listen", ":28081", "listen address")
	indexPath = flag.String("indexpath", "", "path to the index to serve")
	cpuProfile = flag.String("cpuprofile", "", "write cpu profile to this file")
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
	log.Printf("query for %s\n", textQuery)
	re, err := regexp.Compile(textQuery)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
	query := index.RegexpQuery(re.Syntax)
	log.Printf("query: %s\n", query)
	t0 := time.Now()
	post := ix.PostingQuery(query)
	t1 := time.Now()
	log.Printf("query done in %v, %d results\n", t1.Sub(t0), len(post))
	files := make([]string, len(post))
	for idx, fileid := range post {
		files[idx] = ix.Name(fileid)
	}
	t2 := time.Now()
	log.Printf("filenames collected in %v\n", t2.Sub(t1))
	jsonFiles, err := json.Marshal(files)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
	t3 := time.Now()
	log.Printf("marshaling done in %v\n", t3.Sub(t2))
	_, err = w.Write(jsonFiles)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
	t4 := time.Now()
	log.Printf("written in %v\n", t4.Sub(t3))
}

func main() {
	flag.Parse()
	if *indexPath == "" {
		log.Fatal("You need to specify a non-empty -indexpath")
	}
	fmt.Println("Debian Code Search index-backend")

	ix = index.Open(*indexPath)

	http.HandleFunc("/index", Index)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
