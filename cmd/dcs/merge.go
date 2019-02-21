package main

import (
	"flag"
	"log"
	"os"

	"github.com/Debian/dcs/internal/index"
)

const mergeHelp = `merge - merge multiple index files into one

Reads the specified index files, and combines them into one large index.

Example:
  % dcs merge -idx=/tmp/full.idx /tmp/i3.idx /tmp/zsh.idx
`

func merge(args []string) error {
	fset := flag.NewFlagSet("merge", flag.ExitOnError)
	fset.Usage = usage(fset, mergeHelp)
	var idx string
	fset.StringVar(&idx, "idx", "", "path to the index file to work with")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if idx == "" || fset.NArg() == 0 {
		fset.Usage()
		os.Exit(1)
	}

	log.Printf("merging %d files into %s", len(fset.Args()), idx)
	if err := index.ConcatN(idx, fset.Args()); err != nil {
		return err
	}
	return nil
}
