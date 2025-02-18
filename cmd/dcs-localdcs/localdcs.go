package main

import (
	"flag"
	"log"

	"github.com/Debian/dcs/internal/localdcs"
)

func main() {
	flag.Parse()
	instance, err := localdcs.Start(flag.Args()...)
	if err != nil {
		log.Fatal(err)
	}
	if instance.Addr == "" {
		return // stopped
	}
	log.Printf("https://%s", instance.Addr)
}
