package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/Debian/dcs/internal/localdcs"
)

func main() {
	flag.Parse()
	temp, err := os.MkdirTemp("", "dcs-localdcs")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(temp)
	hashKeyPath := filepath.Join(temp, "hash.key")
	if err := os.WriteFile(hashKeyPath, []byte("3270b4d09abccbf3fe59b957b1d429c8c58ac5def079ea4b245f66ade65168c2"), 0600); err != nil {
		log.Fatal(err)
	}
	blockKeyPath := filepath.Join(temp, "block.key")
	if err := os.WriteFile(blockKeyPath, []byte("cdba47f8f82be74175a75ec864aca56d8dcdc5610c88af446005766c6f9e6fd5"), 0600); err != nil {
		log.Fatal(err)
	}
	instance, err := localdcs.Start(hashKeyPath, blockKeyPath, "internal/endtoend/testdata")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("https://%s", instance.Addr)
	select {} // run until cancelled
}
