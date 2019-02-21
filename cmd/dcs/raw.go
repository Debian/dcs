package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Debian/dcs/internal/index"
)

const rawHelp = `raw - print raw (encoded) index data for the specified trigram

Logically, this command calls the trigram command, figures out the data length
and copies the data to stdout.

Example:
  % dcs raw -idx=/srv/dcs/shard4/full -trigram=i3F -section=docid | hd -v
  00000000  87 0c 39 0e 18 0e ff 0e  72 59 18 48 07 ae 64 25  |..9.....rY.H..d%|
  00000010  06 0b 20 08 03 23 00 33  c5 36 3b c7 3f 66 b0 06  |.. ..#.3.6;.?f..|
  00000020  0b 34 0a 02 40 09 cc 80  30 0d 00 f8 81 30 70 59  |.4..@...0....0pY|
  00000030  37 06 82 7e 80 73 28 64  1c 8b d0 f5 2e 0e 04 02  |7..~.s(d........|
  00000040  3d a3 8d 16 08 04 04 0c  13 fc 61 c6 f3 7e 17 43  |=.........a..~.C|
  00000050  fa 08                                             |..|
  00000052
`

func raw(args []string) error {
	fset := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fset.Usage = usage(fset, rawHelp)
	var idx string
	fset.StringVar(&idx, "idx", "", "path to the index file to work with")
	var trigram string
	fset.StringVar(&trigram, "trigram", "", "trigram to read (%c%c%c)")
	var section string
	fset.StringVar(&section, "section", "", "Index section to print (one of docid, pos, posrel)")

	if err := fset.Parse(args); err != nil {
		return err
	}
	if idx == "" || trigram == "" || section == "" {
		fset.Usage()
		os.Exit(1)
	}
	if section != "docid" && section != "pos" && section != "posrel" {
		return fmt.Errorf("invalid -section=%s: expected one of docid, pos, posrel", section)
	}
	if len(trigram) < 3 {
		return fmt.Errorf("invalid -trigram=%s syntax: expected 3 bytes, got %d bytes", trigram, len(trigram))
	}
	t := []byte(trigram)
	tri := index.Trigram(uint32(t[0])<<16 | uint32(t[1])<<8 | uint32(t[2]))

	i, err := index.Open(idx)
	if err != nil {
		return fmt.Errorf("index.Open(%s): %v", idx, err)
	}
	defer i.Close()

	if section == "posrel" {
		_, entries, err := i.Pos.Data(tri)
		if err != nil {
			return fmt.Errorf("Could not locate trigram metadata: %v", err)
		}

		db, err := i.Posrel.DataBytes(tri)
		if err != nil {
			return fmt.Errorf("Posrel.DataBytes(%v): %v", tri, err)
		}
		if _, err := os.Stdout.Write(db[:(entries+7)/8]); err != nil {
			return err
		}
		return nil
	}

	var r io.Reader
	if section == "docid" {
		r, _, err = i.Docid.Data(tri)
	} else if section == "pos" {
		r, _, err = i.Pos.Data(tri)
	}
	if err != nil {
		return fmt.Errorf("Could not locate trigram metadata: %v", err)
	}

	_, err = io.Copy(os.Stdout, r)
	return err
}
