package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Debian/dcs/internal/index"
)

const trigramHelp = `trigram - display metadata of the specified trigram

Example:
  % dcs trigram -idx=/srv/dcs/shard0/full -trigram=i3F -section=docid
  {Trigram:6894406 Entries:29 OffsetData:618935742}
`

func trigram(args []string) error {
	fset := flag.NewFlagSet("trigram", flag.ExitOnError)
	fset.Usage = usage(fset, trigramHelp)
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
		return fmt.Errorf("Could not open index: %v", err)
	}
	defer i.Close()

	var m *index.MetaEntry
	switch section {
	case "docid":
		m, err = i.Docid.MetaEntry(tri)
	case "pos":
		m, err = i.Pos.MetaEntry(tri)
	case "posrel":
		m, err = i.Posrel.MetaEntry(tri)
	}
	if err != nil {
		return fmt.Errorf("Could not locate trigram metadata: %v", err)
	}

	fmt.Printf("%+v\n", *m)
	return nil
}
