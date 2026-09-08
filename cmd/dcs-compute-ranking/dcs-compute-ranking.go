package main

import (
	"flag"
	"log"

	"github.com/Debian/dcs/internal/computeranking"
)

var (
	mirrorURL = flag.String("mirror_url",
		"http://deb.debian.org/debian",
		"URL to the debian mirror to use")

	popconURL = flag.String("popcon_url",
		"https://popcon.debian.org/all-popcon-results.txt.gz",
		"URL to the popcon results file")

	verbose = flag.Bool("verbose",
		false,
		"Print ranking information about every package")

	outputPath = flag.String("output_path",
		"/var/dcs/ranking.json",
		"Path to store the resulting ranking JSON data at. Will be overwritten atomically using rename(2), which also implies that TMPDIR= must point to a directory on the same file system as -output_path.")
)

func main() {
	flag.Parse()

	if err := computeranking.Main(*mirrorURL, *popconURL, *outputPath, *verbose); err != nil {
		log.Fatal(err)
	}
}
