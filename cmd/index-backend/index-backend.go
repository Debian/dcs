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
)

var ix *index.Index

var indexPath *string = flag.String("indexpath", "", "path to the index to serve")

// Handles requests to /index by compiling the q= parameter into a regular
// expression (codesearch/regexp), searching the index for it and returning the
// list of matching filenames in a JSON array.
// TODO: This doesn’t handle file name regular expressions at all yet.
// TODO: errors aren’t properly signaled to the requester
func Index(w http.ResponseWriter, r *http.Request) {
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
	post := ix.PostingQuery(query)
	var files []string
	for _, fileid := range post {
		files = append(files, ix.Name(fileid))
	}
	jsonFiles, err := json.Marshal(files)
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
	if *indexPath == "" {
		log.Fatal("You need to specify a non-empty -indexpath")
	}
	fmt.Println("Debian Code Search index-backend")

	ix = index.Open(*indexPath)

	http.HandleFunc("/index", Index)
	log.Fatal(http.ListenAndServe(":28081", nil))
}
