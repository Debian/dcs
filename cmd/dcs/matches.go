package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Debian/dcs/internal/index"
)

const matchesHelp = `matches - list the filename[:pos] matches for the specified trigram

Example:
  % dcs matches -idx=/srv/dcs/shard4/full -trigram=i3F
  […]
  i3-wm_4.16.1-1/i3-config-wizard/main.c:2259
  i3-wm_4.16.1-1/i3-config-wizard/main.c:2279
  i3-wm_4.16.1-1/i3-input/main.c:1155
  […]
`

func matches(args []string) error {
	fset := flag.NewFlagSet("matches", flag.ExitOnError)
	fset.Usage = usage(fset, matchesHelp)
	var idx string
	fset.StringVar(&idx, "idx", "", "path to the index file to work with")
	var trigram string
	fset.StringVar(&trigram, "trigram", "", "trigram to read (%c%c%c)")
	var namesOnly bool
	fset.BoolVar(&namesOnly, "names", false, "display file names only (no positions)")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if idx == "" || trigram == "" {
		fset.Usage()
		os.Exit(1)
	}
	if len(trigram) < 3 {
		return fmt.Errorf("invalid -trigram=%s syntax: expected 3 bytes, got %d bytes", trigram, len(trigram))
	}
	t := []byte(trigram)
	tri := index.Trigram(uint32(t[0])<<16 | uint32(t[1])<<8 | uint32(t[2]))

	i, err := index.Open(idx)
	if err != nil {
		return fmt.Errorf("Could not open index: %v", err)
	}
	defer i.Close()

	matches, err := i.Matches(tri)
	if err != nil {
		return err
	}
	if namesOnly {
		var last string
		for _, match := range matches {
			fn, err := i.DocidMap.Lookup(match.Docid)
			if err != nil {
				return err
			}
			if fn != last {
				fmt.Println(fn)
				last = fn
			}
		}
		return nil
	}
	for _, match := range matches {
		fn, err := i.DocidMap.Lookup(match.Docid)
		if err != nil {
			return err
		}
		fmt.Printf("%s:%d\n", fn, match.Position)
	}
	return nil
}
