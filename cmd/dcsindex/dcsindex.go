// vim:ts=4:sw=4:noexpandtab
package main

import (
	"fmt"
	"os"
	"regexp"
	"path/filepath"
	"code.google.com/p/codesearch/index"
)

var sourceFiles *regexp.Regexp = regexp.MustCompile(`\.c$`)

func main() {
	fmt.Println("Debian Code Search indexing tool")

	// TODO: make the path configurable
	mirrorPath := "/media/sdg/debian-source-mirror/unpacked/"

	// Walk through all the directories and add files matching our source file
	// regular expression to the index.
	ix := index.Create("/tmp/tmp.idx")
	ix.Verbose = true
	ix.AddPaths([]string{ mirrorPath })

	cnt := 0
	filepath.Walk(mirrorPath, func(path string, info os.FileInfo, err error) error {
		//fmt.Printf("Checking path %s\n", path)
		if sourceFiles.MatchString(path) {
			ix.AddFile(path)
			cnt++
			if cnt > 100 {
				ix.Flush()
				os.Exit(0)
			}
		}
		return nil
	})
}
