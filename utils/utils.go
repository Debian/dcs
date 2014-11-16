package utils 

import (
	"compress/bzip2"
	"github.com/mstap/godebiancontrol"
	"log"
	"os"
	"path/filepath"
)

func MustLoadMirroredControlFile(mirrorPath string, dist string, name string) []godebiancontrol.Paragraph {
	var base = filepath.Join(mirrorPath, "dists", dist)
	files, err := os.Open(base)
	if err != nil {
		log.Fatal(err)
	}
	fi, err := files.Readdir(-1)
	var contents = make([]godebiancontrol.Paragraph, 0)
	for _, file := range fi {
		if !file.IsDir() {
			continue
		}
		file, err := os.Open(filepath.Join(base, file.Name(), name))
		contents_new, err := godebiancontrol.Parse(bzip2.NewReader(file))
		if err != nil {
			log.Fatal(err)
		}
		contents = append(contents, contents_new...)
		defer file.Close()
	}

	return contents
}
