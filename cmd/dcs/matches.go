package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/Debian/dcs/internal/index"
)

const matchesHelp = `matches - list the filename[:pos] matches for the specified trigram

Example:
  % dcs matches -idx=/srv/dcs/full -trigram=i3F

`

func matches(args []string) {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	var idx string
	fs.StringVar(&idx, "idx", "", "path to the index file to work with")
	var trigram string
	fs.StringVar(&trigram, "trigram", "", "trigram to read (%c%c%c)")
	var namesOnly bool
	fs.BoolVar(&namesOnly, "names", false, "display file names only (no positions)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if idx == "" || trigram == "" {
		fs.Usage()
		os.Exit(1)
	}
	if len(trigram) < 3 {
		log.Fatalf("invalid -trigram=%s syntax: expected 3 bytes, got %d bytes", trigram, len(trigram))
	}
	t := []byte(trigram)
	tri := index.Trigram(uint32(t[0])<<16 | uint32(t[1])<<8 | uint32(t[2]))

	i, err := index.Open(idx)
	if err != nil {
		log.Fatalf("Could not open index: %v", err)
	}
	defer i.Close()

	matches, err := i.Matches(tri)
	if err != nil {
		log.Fatal(err)
	}
	if namesOnly {
		var last string
		for _, match := range matches {
			fn, err := i.DocidMap.Lookup(match.Docid)
			if err != nil {
				log.Fatal(err)
			}
			if fn != last {
				fmt.Println(fn)
				last = fn
			}
		}
		return
	}
	for _, match := range matches {
		fn, err := i.DocidMap.Lookup(match.Docid)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s:%d\n", fn, match.Position)
	}
}
