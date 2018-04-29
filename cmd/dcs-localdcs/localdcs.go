package main

import (
	"flag"
	"log"

	"github.com/Debian/dcs/internal/localdcs"
)

func main() {
	flag.Parse()
	if _, err := localdcs.Start(); err != nil {
		log.Fatal(err)
	}
}
