package main

import (
	"flag"
	"log"

	"github.com/Debian/dcs/internal/localdcs"
)

func main() {
	flag.Parse()
	const (
		hashKey  = "268afdf10dfe2bd9bc1aa7ceb3f448071cfb3e06488f02affabb6e8d60ccf994"
		blockKey = "132c2e1cff7c4735f746500bd03780bf9b2aaa1ba7f706b8edb22dedeb39f46e"
	)
	instance, err := localdcs.Start(hashKey, blockKey)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("https://%s", instance.Addr)
	select {} // run until cancelled
}
