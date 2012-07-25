// vim:ts=4:sw=4:noexpandtab
package main

import (
	// This is a forked version of codesearch/regexp which returns the results
	// in a structure instead of printing to stdout/stderr directly.
	"dcs/regexp"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"flag"
)

var grep regexp.Grep = regexp.Grep{
	Stdout: os.Stdout,
	Stderr: os.Stderr,
}

// TODO: we want all filenames at once (so we need to compile the regexp once),
// probably passed via POST to get around GET length limitations?
func Source(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	textQuery := r.Form.Get("q")
	filename := r.Form.Get("filename")
	log.Printf("query for %s in %s\n", textQuery, filename)
	re, err := regexp.Compile(textQuery)
	if err != nil {
		log.Printf("%s\n", err)
		return
	}

	log.Printf("query = %s\n", re)

	grep.Regexp = re
	matches := grep.File(filename)
	jsonFiles, err := json.Marshal(matches)
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
