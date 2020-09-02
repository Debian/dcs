package main

import (
	"flag"
	"log"
	"os"
	"os/exec"

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
	browser := exec.Command("google-chrome", "https://"+instance.Addr)
	browser.Stderr = os.Stderr
	if err := browser.Run(); err != nil {
		log.Printf("%v: %v", browser.Args, err)
	}
}
