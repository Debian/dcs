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

func merge(args []string) {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	var idx string
	fs.StringVar(&idx, "idx", "", "path to the index file to work with")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if idx == "" || fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}

	log.Printf("merging %d files into %s", len(fs.Args()), idx)
	if err := index.ConcatN(idx, fs.Args()); err != nil {
		log.Fatal(err)
	}
}
