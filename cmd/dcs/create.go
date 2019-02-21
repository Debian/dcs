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

func create(args []string) error {
	fset := flag.NewFlagSet("create", flag.ExitOnError)
	fset.Usage = usage(fset, createHelp)
	var idx string
	fset.StringVar(&idx, "idx", "", "path to the index file to work with")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if idx == "" || fset.NArg() != 1 {
		fset.Usage()
		os.Exit(1)
	}

	filter.Init()

	w, err := index.Create(idx)
	if err != nil {
		return err
	}

	if err := w.AddDir(
		fset.Arg(0),
		filepath.Clean(fset.Arg(0))+"/",
		filter.Ignored,
		func(path string, _ os.FileInfo, err error) error {
			log.Printf("skipping %q: %v", path, err)
			return nil
		},
		nil,
	); err != nil {
		return err
	}

	if err := w.Flush(); err != nil {
		return err
	}
	return nil
}
