package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/Debian/dcs/internal/filter"
	"github.com/Debian/dcs/internal/index"
)

const createHelp = `create - create index from specified directory

Creates a Debian Code Search index for the specified directory.

Note that a number of files who don’t add much value (e.g. auto-generated
configure scripts) are ignored by DCS.

Example:
  % dcs create -idx=/tmp/i3.idx ~/i3

`

func create(args []string) {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	var idx string
	fs.StringVar(&idx, "idx", "", "path to the index file to work with")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if idx == "" || fs.NArg() != 1 {
		fs.Usage()
		os.Exit(1)
	}

	filter.Init()

	w, err := index.Create(idx)
	if err != nil {
		log.Fatal(err)
	}

	if err := w.AddDir(
		fs.Arg(0),
		filepath.Clean(fs.Arg(0))+"/",
		filter.Ignored,
		func(path string, _ os.FileInfo, err error) error {
			log.Printf("skipping %q: %v", path, err)
			return nil
		},
		nil,
	); err != nil {
		log.Fatal(err)
	}

	if err := w.Flush(); err != nil {
		log.Fatal(err)
	}
}
