// vim:ts=4:sw=4:noexpandtab

// Indexing tool for Debian Code Search. The tool scans through an unpacked
// source package mirror and adds relevant files to the regular expression
// trigram index.
//
// One run takes about 3 hours when using a slow USB disk as TMPDIR and storing
// the index output on a SSD.
package main

import (
	"code.google.com/p/codesearch/index"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var numShards = flag.Int("shards", 1,
	"Number of index shards (the index will be split into 'shard' different files)")
var mirrorPath = flag.String("mirrorpath",
	"/dcs-ssd/",
	"Path to an Debian source package mirror with an /unpacked directory inside")

// Returns true when the file matches .[0-9]$ (cheaper than a regular
// expression).
func hasManpageSuffix(filename string) bool {
	return len(filename) > 2 &&
		filename[len(filename)-2] == '.' &&
		filename[len(filename)-1] >= '0' &&
		filename[len(filename)-1] <= '9'
}

func main() {
	flag.Parse()
	fmt.Println("Debian Code Search indexing tool")

	// Walk through all the directories and add files matching our source file
	// regular expression to the index.
	ix := make([]*index.IndexWriter, *numShards)
	for i := 0; i < *numShards; i++ {
		path := fmt.Sprintf("%s/index.%d.idx", *mirrorPath, i)
		ix[i] = index.Create(path)
		ix[i].Verbose = true
	}

	// XXX: This is actually only for house-keeping. Not sure if we will use it for anything.
	//ix.AddPaths([]string{ mirrorPath })

	cnt := 0
	filepath.Walk(path.Join(*mirrorPath, "unpacked"),
		func(path string, info os.FileInfo, err error) error {
			if _, filename := filepath.Split(path); filename != "" {
				// Skip quilt’s .pc directories and "po" directories (localization)
				if info.IsDir() &&
					(filename == ".pc" ||
						filename == "po") {
					return filepath.SkipDir
				}

				// NB: we don’t skip "configure" since that might be a custom shell-script
				// Skip documentation, configuration files and patches.
				if filename == "NEWS" ||
					filename == "COPYING" ||
					filename == "LICENSE" ||
					strings.HasSuffix(filename, ".conf") ||
					// spell checking dictionaries
					strings.HasSuffix(filename, ".dic") ||
					strings.HasSuffix(filename, ".cfg") ||
					strings.HasSuffix(filename, ".man") ||
					strings.HasSuffix(filename, ".xml") ||
					strings.HasSuffix(filename, ".xsl") ||
					strings.HasSuffix(filename, ".html") ||
					strings.HasSuffix(filename, ".sgml") ||
					strings.HasSuffix(filename, ".pod") ||
					strings.HasSuffix(filename, ".po") ||
					strings.HasSuffix(filename, ".patch") ||
					strings.HasSuffix(filename, ".txt") ||
					strings.HasSuffix(filename, ".tex") ||
					strings.HasSuffix(filename, ".rtf") ||
					strings.HasSuffix(filename, ".docbook") ||
					strings.HasSuffix(filename, ".symbols") ||
					strings.HasPrefix(strings.ToLower(filename), "changelog") ||
					strings.HasPrefix(strings.ToLower(filename), "readme") ||
					hasManpageSuffix(filename) {
					return nil
				}
			}
			if info != nil && info.Mode()&os.ModeType == 0 {
				ix[cnt%*numShards].AddFile(path)
				cnt++
			}
			return nil
		})
	for i := 0; i < *numShards; i++ {
		ix[i].Flush()
	}
	os.Exit(0)
}
