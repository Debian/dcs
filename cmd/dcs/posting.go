package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Debian/dcs/internal/index"
)

const postingHelp = `posting - list the (decoded) posting list for the specified trigram

Example:
  % dcs posting -idx=/srv/dcs/shard0/full -trigram=i3F -section=docid
  64747
  64932
  181029
  […]

  % dcs posting -idx=/srv/dcs/shard0/full -trigram=i3F -section=docid -deltas
  64747
  185
  116097
  […]
`

func posting(args []string) error {
	fset := flag.NewFlagSet("posting", flag.ExitOnError)
	fset.Usage = usage(fset, postingHelp)
	var idx string
	fset.StringVar(&idx, "idx", "", "path to the index file to work with")
	var trigram string
	fset.StringVar(&trigram, "trigram", "", "trigram to read (%c%c%c)")
	var section string
	fset.StringVar(&section, "section", "", "Index section to print (one of docid, pos)")
	var rawDeltas bool
	fset.BoolVar(&rawDeltas, "deltas", false, "display the raw deltas instead of decoding them")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if idx == "" || trigram == "" || section == "" {
		fset.Usage()
		os.Exit(1)
	}
	if section != "docid" && section != "pos" {
		return fmt.Errorf("invalid -section=%s: expected one of docid, pos", section)
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

	var deltas []uint32
	if section == "docid" {
		deltas, err = i.Docid.Deltas(tri)
	} else {
		deltas, err = i.Pos.Deltas(tri)
	}
	if err != nil {
		return fmt.Errorf("Getting trigram deltas for section %s: %v", section, err)
	}
	if !rawDeltas {
		var prev uint32
		for i, delta := range deltas {
			delta += prev
			deltas[i] = delta
			prev = delta
		}
	}
	for _, delta := range deltas {
		fmt.Println(delta)
	}
	return nil
}
