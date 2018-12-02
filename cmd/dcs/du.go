package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
)

const duHelp = `du — shows disk usage of the specified index files

Think du(1), but for index files, optionally including positional sections.

Example:
  % dcs du -h /srv/dcs/shard0/full
  % dcs du -h /srv/dcs/shard*/full
  % dcs du -h -pos /srv/dcs/shard*/full
  % dcs du -h -pos /srv/dcs/shard*/idx/* # per-package

`

func humanReadableBytes(v int64) string {
	switch {
	case v > (1024 * 1024 * 1024):
		return fmt.Sprintf("%.1fG", float64(v)/1024/1024/1024)
	case v > (1024 * 1024):
		return fmt.Sprintf("%.1fM", float64(v)/1024/1024)
	case v > 1024:
		return fmt.Sprintf("%.1fK", float64(v)/1024)
	default:
		return fmt.Sprintf("%d", v)
	}
}

func measure(dir string, pos bool) (int64, error) {
	fis, err := ioutil.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, fi := range fis {
		if !pos && strings.Contains(fi.Name(), "posting.pos") {
			continue
		}
		total += fi.Size()
	}
	return total, nil
}

func du(args []string) error {
	fset := flag.NewFlagSet("du", flag.ExitOnError)
	var (
		pos = fset.Bool("pos", false, "measure positional index section, too")
		h   = fset.Bool("h", false, "human readable output")
	)
	if err := fset.Parse(args); err != nil {
		return err
	}
	if fset.NArg() < 1 {
		return fmt.Errorf("Usage: du <index> [<index>…]")
	}
	format := func(v int64) string { return fmt.Sprintf("%d", v) }
	if *h {
		format = humanReadableBytes
	}
	var total int64
	for _, dir := range fset.Args() {
		size, err := measure(dir, *pos)
		if err != nil {
			return err
		}
		log.Printf("%v %s", format(size), dir)
		total += size
	}
	log.Printf("%v total", format(total))
	return nil
}

// disk layout (for an individual shard):
// /srv/dcs/src/<debsrcpkg>  - extracted Debian source package contents
// /srv/dcs/idx/<debsrcpkg>  - index of /srv/dcs/src/<debsrcpkg>
// /srv/dcs/full/            - merged /srv/dcs/idx/*
// → -root=/srv/dcs (global?) flag

// TODO: which stats to display?
