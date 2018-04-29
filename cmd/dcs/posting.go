package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/Debian/dcs/internal/index"
)

const postingHelp = `posting - list the (decoded) posting list for the specified trigram

Example:
  % dcs posting -idx=/srv/dcs/full -trigram=i3F -section=docid
  % dcs posting -idx=/srv/dcs/full -trigram=i3F -section=docid -deltas

`

func posting(args []string) {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	var idx string
	fs.StringVar(&idx, "idx", "", "path to the index file to work with")
	var trigram string
	fs.StringVar(&trigram, "trigram", "", "trigram to read (%c%c%c)")
	var section string
	fs.StringVar(&section, "section", "", "Index section to print (one of docid, pos)")
	var rawDeltas bool
	fs.BoolVar(&rawDeltas, "deltas", false, "display the raw deltas instead of decoding them")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if idx == "" || trigram == "" || section == "" {
		fs.Usage()
		os.Exit(1)
	}
	if section != "docid" && section != "pos" {
		log.Fatalf("invalid -section=%s: expected one of docid, pos", section)
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

	var deltas []uint32
	if section == "docid" {
		deltas, err = i.Docid.Deltas(tri)
	} else {
		deltas, err = i.Pos.Deltas(tri)
	}
	if err != nil {
		log.Fatalf("Getting trigram deltas for section %s: %v", section, err)
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
}
