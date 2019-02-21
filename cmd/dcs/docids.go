package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/Debian/dcs/internal/index"
)

const docidsHelp = `docids - list the documents covered by this index

Example:
  % dcs docids -idx=/srv/dcs/full
  % dcs docids -idx=/srv/dcs/full -doc=3273
`

func docids(args []string) error {
	fset := flag.NewFlagSet("docids", flag.ExitOnError)
	fset.Usage = usage(fset, docidsHelp)
	var idx string
	fset.StringVar(&idx, "idx", "", "path to the index file to work with")
	var doc int
	fset.IntVar(&doc, "doc", -1, "docid of the document to display. All docids are displayed if set to -1 (the default)")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if idx == "" {
		fset.Usage()
		os.Exit(1)
	}
	i, err := index.Open(idx)
	if err != nil {
		return fmt.Errorf("Could not open index: %v", err)
	}
	defer i.Close()

	if doc > -1 {
		// Display a specific docid entry
		if doc > math.MaxUint32 {
			return fmt.Errorf("-doc=%d exceeds the uint32 docid space", doc)
		}
		fn, err := i.DocidMap.Lookup(uint32(doc))
		if err != nil {
			return err
		}
		fmt.Println(fn)
		return nil
	}
	// Display all docid entries
	if _, err := io.Copy(os.Stdout, i.DocidMap.All()); err != nil {
		return err
	}
	return nil
}
