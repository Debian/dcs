// vim:ts=4:sw=4:noexpandtab
package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/stapelberg/godebiancontrol"
)

var (
	mirrorUrl = flag.String("mirror_url",
		"http://httpredir.debian.org/debian",
		"URL to the debian mirror to use")

	verbose = flag.Bool("verbose",
		false,
		"Print ranking information about every package")

	outputPath = flag.String("output_path",
		"/var/dcs/ranking.json",
		"Path to store the resulting ranking JSON data at. Will be overwritten atomically using rename(2), which also implies that TMPDIR= must point to a directory on the same file system as -output_path.")
)

func mustLoadMirroredControlFile(name string) []godebiancontrol.Paragraph {
	url := fmt.Sprintf("%s/dists/sid/main/%s", *mirrorUrl, name)
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode != 200 {
		log.Fatalf("URL %q resulted in %v\n", url, resp.Status)
	}
	defer resp.Body.Close()

	reader, err := gzip.NewReader(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	contents, err := godebiancontrol.Parse(reader)
	if err != nil {
		log.Fatal(err)
	}

	return contents
}

func main() {
	flag.Parse()

	sourcePackages := mustLoadMirroredControlFile("source/Sources.gz")
	binaryPackages := mustLoadMirroredControlFile("binary-amd64/Packages.gz")

	popconInstSrc, err := popconInstallations(binaryPackages)
	if err != nil {
		log.Fatal(err)
	}
	// Normalize the installation count.
	var totalInstallations float32
	for _, insts := range popconInstSrc {
		totalInstallations += insts
	}
	for srcpkg, insts := range popconInstSrc {
		// We multiply 1000 here because all values are < 0.0009.
		popconInstSrc[srcpkg] = (insts / totalInstallations) * 1000
	}

	reverseDeps := make(map[string]uint)
	for _, pkg := range binaryPackages {
		// We need to filter duplicates, because consider this:
		// agda-bin Recommends: libghc-agda-dev (>= 2.3.2), libghc-agda-dev (<< 2.3.2)
		dependsOn := make(map[string]bool)
		// NB: This differs from what apt-cache rdepends spit out. apt-cache
		// also considers the Replaces field.
		allDeps := pkg["Depends"] + "," + pkg["Suggests"] + "," + pkg["Recommends"] + "," + pkg["Enhances"]
		for _, dep := range strings.FieldsFunc(allDeps, func(r rune) bool {
			return r == ',' || r == '|'
		}) {
			trimmed := strings.TrimSpace(dep)
			spaceIdx := strings.Index(trimmed, " ")
			if spaceIdx == -1 {
				spaceIdx = len(trimmed)
			}
			dependsOn[trimmed[:spaceIdx]] = true
		}
		for name, _ := range dependsOn {
			reverseDeps[name] += 1
		}
	}

	type storedRanking struct {
		Inst float32
		Rdep float32
	}
	rankings := make(map[string]storedRanking)

	for _, pkg := range sourcePackages {
		rdepcount := float32(0)
		for _, packageName := range strings.Split(pkg["Binary"], ",") {
			packageName = strings.TrimSpace(packageName)
			if packageName == "" {
				continue
			}
			rdepcount += float32(reverseDeps[packageName])
		}
		srcpkg := pkg["Package"]
		packageRank := popconInstSrc[srcpkg]
		rdepcount = 1.0 - (1.0 / float32(rdepcount+1))
		if *verbose {
			fmt.Printf("%f %f %s\n", packageRank, rdepcount, srcpkg)
		}
		rankings[srcpkg] = storedRanking{packageRank, rdepcount}
	}

	f, err := ioutil.TempFile(filepath.Dir(*outputPath), "dcs-compute-ranking")
	if err != nil {
		log.Fatal(err)
	}

	if err := json.NewEncoder(f).Encode(rankings); err != nil {
		log.Fatal(err)
	}

	if err := f.Close(); err != nil {
		log.Fatal(err)
	}

	if err := os.Rename(f.Name(), *outputPath); err != nil {
		log.Fatal(err)
	}
}
