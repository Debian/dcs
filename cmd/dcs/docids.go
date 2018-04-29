package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"

	"github.com/Debian/dcs/internal/index"
)

const docidsHelp = `docids - list the documents covered by this index

Example:
  % dcs docids -idx=/srv/dcs/full
  % dcs docids -idx=/srv/dcs/full -doc=3273

`

func docids(args []string) {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	var idx string
	fs.StringVar(&idx, "idx", "", "path to the index file to work with")
	var doc int
	fs.IntVar(&doc, "doc", -1, "docid of the document to display. All docids are displayed if set to -1 (the default)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if idx == "" {
		fs.Usage()
		os.Exit(1)
	}
	i, err := index.Open(idx)
	if err != nil {
		log.Fatalf("Could not open index: %v", err)
	}
	defer i.Close()

	if doc > -1 {
		// Display a specific docid entry
		if doc > math.MaxUint32 {
			log.Fatalf("-doc=%d exceeds the uint32 docid space", doc)
		}
		fn, err := i.DocidMap.Lookup(uint32(doc))
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(fn)
		return
	}
	// Display all docid entries
	if _, err := io.Copy(os.Stdout, i.DocidMap.All()); err != nil {
		log.Fatal(err)
	}
}
