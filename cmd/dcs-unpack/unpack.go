package main

import (
	"compress/bzip2"
	"flag"
	"fmt"
	"github.com/mstap/godebiancontrol"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var mirrorPath = flag.String("mirrorPath",
	"/media/sdd1/debian-source-mirror/",
	"Path to the debian source mirror (which contains the 'dists' and 'pool' folder)")
var oldUnpackPath = flag.String("oldUnpackPath",
	"/dcs-ssd/unpacked/",
	"Path to the unpacked debian source mirror")
var newUnpackPath = flag.String("newUnpackPath",
	"/dcs-ssd/unpacked-new/",
	"Path to the unpacked debian source mirror")

var removeEpoch = regexp.MustCompile(`^[0-9]+:`)

// Copies directories by hard-linking all files inside,
// necessary since hard-links on directories are not possible.
//
// Files/Directories which already exist do not cause errors so that dcs-unpack
// can be run multiple times and will resume where it stopped.
func linkDirectory(oldPath, newPath string) error {
	fmt.Printf("linking %s\n", oldPath)
	return filepath.Walk(oldPath, func(path string, info os.FileInfo, err error) error {
		newName := strings.Replace(path, oldPath, newPath, 1)
		if info.Mode().IsDir() {
			if err := os.Mkdir(newName, info.Mode()); err != nil && !os.IsExist(err) {
				return err
			}
		} else {
			if err := os.Link(path, newName); err != nil && !os.IsExist(err) {
				return err
			}
		}
		return nil
	})
}

func main() {
	flag.Parse()

	// Walk through all source packages
	file, err := os.Open(path.Join(*mirrorPath, "/dists/sid/main/source/Sources.bz2"))
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	sourcePackages, err := godebiancontrol.Parse(bzip2.NewReader(file))
	if err != nil {
		log.Fatal(err)
	}

	if err := os.Mkdir(*newUnpackPath, 0775); err != nil && !os.IsExist(err) {
		log.Fatal(err)
	}

	for _, pkg := range sourcePackages {
		// Skip packages ending in -data as they don’t contain source code
		// (this is a Debian convention only)
		if strings.HasSuffix(pkg["Package"], "-data") {
			continue
		}

		// The epoch is not used by dpkg-source when unpacking, so remove it
		version := removeEpoch.ReplaceAllString(pkg["Version"], "")

		dir := fmt.Sprintf("%s_%s", pkg["Package"], version)
		oldPath := path.Join(*oldUnpackPath, dir)
		newPath := path.Join(*newUnpackPath, dir)

		// Check whether the directory exists in the old "unpacked" directory
		if _, err := os.Stat(oldPath); err == nil {
			log.Printf("hardlink %s\n", dir)
			// If so, just hardlink it to save space and computing time.
			if err := linkDirectory(oldPath, newPath); err != nil {
				log.Fatal(err)
			}
		} else {
			log.Printf("unpack %s\n", dir)
			files := strings.Split(pkg["Files"], "\n")
			filepath := ""
			for _, line := range files {
				if !strings.HasSuffix(line, ".dsc") {
					continue
				}

				parts := strings.Split(line, " ")
				file := parts[len(parts)-1]
				prefix := string(file[0])
				if strings.HasPrefix(file, "lib") {
					prefix = "lib" + string(file[3])
				}
				filepath = path.Join(*mirrorPath, "pool", "main", prefix, pkg["Package"], file)
			}

			if filepath == "" {
				log.Fatalf("Package %s contains no dsc file, cannot unpack\n", pkg["Package"])
			}

			// Verify the file exists, just in case our algorithm of putting
			// together the full file path diverges from what Debian does.
			if _, err := os.Stat(filepath); err != nil {
				log.Fatal(err)
			}

			cmd := exec.Command("dpkg-source", "--no-copy", "--no-check", "-x", filepath, newPath)
			if err := cmd.Run(); err != nil {
				log.Fatal(err)
			}
		}
	}
}
