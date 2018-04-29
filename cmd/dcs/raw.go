package main

import (
	"flag"
	"io"
	"log"
	"os"

	"github.com/Debian/dcs/internal/index"
)

const rawHelp = `raw - print raw (encoded) index data for the specified trigram

Logically, this command calls the trigram command, figures out the data length
and copies the data to stdout.

Example:
  % dcs raw -idx=/srv/dcs/full -trigram=i3F -section=docid | hd -v

`

func raw(args []string) {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	var idx string
	fs.StringVar(&idx, "idx", "", "path to the index file to work with")
	var trigram string
	fs.StringVar(&trigram, "trigram", "", "trigram to read (%c%c%c)")
	var section string
	fs.StringVar(&section, "section", "", "Index section to print (one of docid, pos, posrel)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if idx == "" || trigram == "" || section == "" {
		fs.Usage()
		os.Exit(1)
	}
	if section != "docid" && section != "pos" && section != "posrel" {
		log.Fatalf("invalid -section=%s: expected one of docid, pos, posrel", section)
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

	if section == "posrel" {
		_, entries, err := i.Pos.Data(tri)
		if err != nil {
			log.Fatalf("Could not locate trigram metadata: %v", err)
		}

		ra, err := i.Posrel.Data(tri)
		if err != nil {
			log.Fatalf("Could not locate trigram metadata: %v", err)
		}
		b := make([]byte, (entries+7)/8)
		if _, err := ra.ReadAt(b, 0); err != nil {
			log.Fatal(err)
		}
		if _, err := os.Stdout.Write(b); err != nil {
			log.Fatal(err)
		}
		return
	}

	var r io.Reader
	if section == "docid" {
		r, _, err = i.Docid.Data(tri)
	} else if section == "pos" {
		r, _, err = i.Pos.Data(tri)
	}
	if err != nil {
		log.Fatalf("Could not locate trigram metadata: %v", err)
	}

	if _, err := io.Copy(os.Stdout, r); err != nil {
		log.Fatal(err)
	}
}
