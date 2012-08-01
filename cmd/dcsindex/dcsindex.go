// vim:ts=4:sw=4:noexpandtab
package main

import (
	"code.google.com/p/codesearch/index"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var patchDir *regexp.Regexp = regexp.MustCompile(`/\.pc/`)
var numShards *int = flag.Int("shards", 1, "Number of index shards (the index will be split into 'shard' different files)")

func main() {
	flag.Parse()
	fmt.Println("Debian Code Search indexing tool")

	// TODO: make the path configurable
	mirrorPath := "/media/sdg/debian-source-mirror/unpacked/"

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
	filepath.Walk(mirrorPath, func(path string, info os.FileInfo, err error) error {
		//fmt.Printf("Checking path %s\n", path)
		if patchDir.MatchString(path) {
			return nil
		}
		ix[cnt % *numShards].AddFile(path)
		cnt++
		return nil
	})
	for i := 0; i < *numShards; i++ {
		ix[i].Flush()
	}
	os.Exit(0)
}
