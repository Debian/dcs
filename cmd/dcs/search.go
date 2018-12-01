package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"regexp/syntax"

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
	var pos bool
	fs.BoolVar(&pos, "pos", false, "do a positional query for identifier searches")
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
	s := re.Syntax.Simplify()
	queryPos := pos && s.Op == syntax.OpLiteral

	ix, err := index.Open(idx)
	if err != nil {
		log.Fatalf("Could not open index: %v", err)
	}
	defer ix.Close()

	if queryPos {
		matches, err := ix.QueryPositional(string(s.Rune))
		if err != nil {
			log.Fatal(err)
		}
		for _, match := range matches {
			fn, err := ix.DocidMap.Lookup(match.Docid)
			if err != nil {
				log.Fatalf("DocidMap.Lookup(%v): %v", match.Docid, err)
			}
			fmt.Printf("%s\n", fn)
			// TODO: actually verify the search term occurs at match.Position
		}
	} else {
		q := index.RegexpQuery(re.Syntax)
		log.Printf("q = %v", q)
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
}
