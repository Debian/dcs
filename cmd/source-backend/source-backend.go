// vim:ts=4:sw=4:noexpandtab
package main

import (
	// This is a forked version of codesearch/regexp which returns the results
	// in a structure instead of printing to stdout/stderr directly.
	"github.com/Debian/dcs/ranking"
	"github.com/Debian/dcs/regexp"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
)

var (
	listenAddress = flag.String("listen_address", ":28082", "listen address ([host]:port)")
	unpackedPath  = flag.String("unpacked_path",
		"/dcs-ssd/unpacked/",
		"Path to the unpacked sources")
)

type SourceReply struct {
	// The number of the last used filename, needed for pagination
	LastUsedFilename int

	AllMatches []regexp.Match
}

// Serves a single file for displaying it in /show
func File(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	filename := r.Form.Get("file")

	log.Printf("requested filename *%s*\n", filename)
	// path.Join calls path.Clean so we get the shortest path without any "..".
	absPath := path.Join(*unpackedPath, filename)
	log.Printf("clean, absolute path is *%s*\n", absPath)
	if !strings.HasPrefix(absPath, *unpackedPath) {
		http.Error(w, "Path traversal is bad, mhkay?", http.StatusForbidden)
		return
	}

	file, err := os.Open(absPath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`Could not open file "%s"`, absPath), http.StatusNotFound)
		return
	}
	defer file.Close()

	io.Copy(w, file)
}

func Source(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	textQuery := r.Form.Get("q")
	limit, err := strconv.ParseInt(r.Form.Get("limit"), 10, 0)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
	filenames := r.Form["filename"]
	re, err := regexp.Compile(textQuery)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}

	log.Printf("query: text = %s, regexp = %s\n", textQuery, re)

	rankingopts := ranking.RankingOptsFromQuery(r.URL.Query())

	querystr := ranking.NewQueryStr(textQuery)

	workers := runtime.NumCPU()
	numfiles := int(math.Ceil(float64(len(filenames)) / float64(workers)))
	workerInputs := make([]chan string, workers)
	workerOutputs := make([]chan []regexp.Match, workers)

	log.Printf("putting %d queries into %d workers each\n", numfiles, workers)

	for i := 0; i < workers; i++ {
		workerInputs[i] = make(chan string, numfiles)
		workerOutputs[i] = make(chan []regexp.Match, numfiles)
		go func(input chan string, output chan []regexp.Match) {
			// TODO: figure out how to safely clone a dcs/regexp
			re, err := regexp.Compile(textQuery)
			if err != nil {
				log.Printf("%s\n", err)
				return
			}

			grep := regexp.Grep{
				Regexp: re,
				Stdout: os.Stdout,
				Stderr: os.Stderr,
			}

			for filename := range input {
				output <- grep.File(path.Join(*unpackedPath, filename))
			}
			close(output)
		}(workerInputs[i], workerOutputs[i])
	}

	for idx, filename := range filenames {
		//log.Printf("sending idx %d to worker %d\n", idx, idx % workers)
		workerInputs[idx % workers] <- filename
	}
	for i := 0; i < workers; i++ {
		close(workerInputs[i])
	}

	fmt.Printf("done, now getting the results\n")

	// TODO: also limit the number of matches per source-package, not only per file
	var reply SourceReply
	for idx, filename := range filenames {
		fmt.Printf("…in %s\n", filename)
		matches := <-workerOutputs[idx % workers]
		for idx, match := range matches {
			if limit > 0 && idx == 5 {
				// TODO: we somehow need to signal that there are more results
				// (if there are more), so that the user can expand this.
				break
			}
			fmt.Printf("match: %s\n", match)
			match.Ranking = ranking.PostRank(rankingopts, &match, &querystr)
			match.Path = match.Path[len(*unpackedPath):]
			reply.AllMatches = append(reply.AllMatches, match)
		}
		if limit > 0 && int64(len(reply.AllMatches)) >= limit {
			reply.LastUsedFilename = idx
			break
		}
	}
	jsonFiles, err := json.Marshal(&reply)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}
	_, err = w.Write(jsonFiles)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}

}

func main() {
	flag.Parse()
	fmt.Println("Debian Code Search source-backend")

	http.HandleFunc("/source", Source)
	http.HandleFunc("/file", File)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
