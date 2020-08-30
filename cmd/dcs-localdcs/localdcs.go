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
	dcsWeb, err := localdcs.Start(flag.Args()...)
	if err != nil {
		log.Fatal(err)
	}
	if dcsWeb == "" {
		return // stopped
	}
	browser := exec.Command("google-chrome", "https://"+dcsWeb)
	browser.Stderr = os.Stderr
	if err := browser.Run(); err != nil {
		log.Printf("%v: %v", browser.Args, err)
	}
}
