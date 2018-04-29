package main

import (
	"flag"
	"log"
	"os"
)

func stats(args []string) {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if fs.NArg() < 1 {
		log.Fatalf("Usage: du <index> [<index>…]")
	}
	for _, dir := range fs.Args() {
		log.Printf("TODO: %s", dir)
	}
}

// disk layout (for an individual shard):
// /srv/dcs/src/<debsrcpkg>  - extracted Debian source package contents
// /srv/dcs/idx/<debsrcpkg>  - index of /srv/dcs/src/<debsrcpkg>
// /srv/dcs/full/            - merged /srv/dcs/idx/*
// → -root=/srv/dcs (global?) flag

// TODO: which stats to display?
