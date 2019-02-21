package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"strings"
)

const duHelp = `du — shows disk usage of the specified index files

Think du(1), but for index files, optionally including positional sections.

Example:
  % dcs du -h /srv/dcs/shard0/full
  981.0M /srv/dcs/shard0/full
  981.0M total

  % dcs du -h /srv/dcs/shard*/full
  981.0M /srv/dcs/shard0/full
  1.0G /srv/dcs/shard1/full
  1.2G /srv/dcs/shard2/full
  1.2G /srv/dcs/shard3/full
  1.6G /srv/dcs/shard4/full
  1.3G /srv/dcs/shard5/full
  7.3G total

  % dcs du -h -pos /srv/dcs/shard*/full
  14.8G /srv/dcs/shard0/full
  15.7G /srv/dcs/shard1/full
  16.3G /srv/dcs/shard2/full
  19.9G /srv/dcs/shard3/full
  24.1G /srv/dcs/shard4/full
  19.3G /srv/dcs/shard5/full
  110.0G total

  % dcs du -h -pos /srv/dcs/shard*/idx/* # per-package
  […]
  4.4M /srv/dcs/shard5/idx/zypper_1.14.11-1
  3.9M /srv/dcs/shard5/idx/zziplib_0.13.62-3.1
  595.0K /srv/dcs/shard5/idx/zzzeeksphinx_1.0.20-2
  150.7K /srv/dcs/shard5/idx/zzz-to-char_0.1.3-1
  143.0G total
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
	fset.Usage = usage(fset, duHelp)
	var pos bool
	fset.BoolVar(&pos, "pos", false, "measure positional index section, too")
	var h bool
	fset.BoolVar(&h, "h", false, "human readable output")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if fset.NArg() < 1 {
		return fmt.Errorf("Usage: du <index> [<index>…]")
	}
	format := func(v int64) string { return fmt.Sprintf("%d", v) }
	if h {
		format = humanReadableBytes
	}
	var total int64
	for _, dir := range fset.Args() {
		size, err := measure(dir, pos)
		if err != nil {
			return err
		}
		fmt.Printf("%v %s\n", format(size), dir)
		total += size
	}
	fmt.Printf("%v total\n", format(total))
	return nil
}

// disk layout (for an individual shard):
// /srv/dcs/src/<debsrcpkg>  - extracted Debian source package contents
// /srv/dcs/idx/<debsrcpkg>  - index of /srv/dcs/src/<debsrcpkg>
// /srv/dcs/full/            - merged /srv/dcs/idx/*
// → -root=/srv/dcs (global?) flag

// TODO: which stats to display?
