// vim:ts=4:sw=4:noexpandtab
package main

import (
	// This is a forked version of codesearch/regexp which returns the results
	// in a structure instead of printing to stdout/stderr directly.
	"dcs/regexp"
	"dcs/ranking"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"flag"
	"strconv"
)

var grep regexp.Grep = regexp.Grep{
	Stdout: os.Stdout,
	Stderr: os.Stderr,
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
	log.Printf("query for %s\n", textQuery)
	re, err := regexp.Compile(textQuery)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}

	log.Printf("query = %s\n", re)

	grep.Regexp = re

// TODO: also limit the number of matches per source-package, not only per file
	var allMatches []regexp.Match
	for _, filename := range filenames {
		log.Printf("…in %s\n", filename)
		matches := grep.File(filename)
		for idx, match := range matches {
			if limit > 0 && idx == 5 {
				// TODO: we somehow need to signal that there are more results
				// (if there are more), so that the user can expand this.
				break
			}
			log.Printf("match: %s", match)
			match.Ranking = ranking.PostRank(&match)
			allMatches = append(allMatches, match)
		}
		if limit > 0 && int64(len(allMatches)) >= limit {
			break
		}
	}
	jsonFiles, err := json.Marshal(allMatches)
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
	grep.AddFlags()
	flag.Parse()
	fmt.Println("Debian Code Search source-backend")

	http.HandleFunc("/source", Source)
	log.Fatal(http.ListenAndServe(":28082", nil))
}
