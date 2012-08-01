// vim:ts=4:sw=4:noexpandtab

// Indexing tool for Debian Code Search. The tool scans through an unpacked
// source package mirror and adds relevant files to the regular expression
// trigram index.
package main

import (
	"code.google.com/p/codesearch/index"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var numShards = flag.Int("shards", 1,
	"Number of index shards (the index will be split into 'shard' different files)")
var mirrorPath = flag.String("mirrorpath",
	"/media/sdg/debian-source-mirror/unpacked/",
	"Path to an unpacked Debian source package mirror")

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
		path := fmt.Sprintf("/media/sdg/debian-source-mirror/index.%d.idx", i)
		ix[i] = index.Create(path)
		ix[i].Verbose = true
	}

	// XXX: This is actually only for house-keeping. Not sure if we will use it for anything.
	//ix.AddPaths([]string{ mirrorPath })

	cnt := 0
	filepath.Walk(*mirrorPath, func(path string, info os.FileInfo, err error) error {
		if _, filename := filepath.Split(path); filename != "" {
			// Skip quilt’s .pc directories
			if filename == ".pc" && info.IsDir() {
				return filepath.SkipDir
			}

			// Skip documentation, configuration files and patches.
			if filename == "README" ||
				filename == "NEWS" ||
				strings.HasSuffix(filename, ".man") ||
				strings.HasSuffix(filename, ".xml") ||
				strings.HasSuffix(filename, ".html") ||
				strings.HasSuffix(filename, ".pod") ||
				strings.HasSuffix(filename, ".patch") ||
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
