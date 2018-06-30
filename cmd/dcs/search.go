package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/Debian/dcs/internal/index"
	"github.com/google/codesearch/regexp"
)

const searchHelp = `search - list the filename[:pos] matches for the specified search query

Example:
  % dcs search -idx=/srv/dcs/full -query=i3Font

`

func search(args []string) {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	var idx string
	fs.StringVar(&idx, "idx", "", "path to the index file to work with")
	var query string
	fs.StringVar(&query, "query", "", "search query")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if idx == "" || query == "" {
		fs.Usage()
		os.Exit(1)
	}

	log.Printf("search for %q", query)
	re, err := regexp.Compile(query)
	if err != nil {
		log.Fatalf("regexp.Compile(%q): %v", query, err)
	}
	q := index.RegexpQuery(re.Syntax)
	log.Printf("q = %v", q)

	ix, err := index.Open(idx)
	if err != nil {
		log.Fatalf("Could not open index: %v", err)
	}
	defer ix.Close()

	// TODO: do an identifier query if possible

	docids := ix.PostingQuery(q)
	for _, docid := range docids {
		fn, err := ix.DocidMap.Lookup(docid)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s\n", fn)
		// TODO: actually grep the file to find a match
	}
}
