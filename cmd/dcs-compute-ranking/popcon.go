package main

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/stapelberg/godebiancontrol"
)

var (
	asciiMatch = regexp.MustCompile(`^(?P<package>[A-Za-z0-9-.+_]+)(:(?P<arch>[a-z0-9]+))?$`)
)

func popconInstallations(binaryPackages []godebiancontrol.Paragraph) (map[string]float32, error) {
	binaryToSource := make(map[string]string)
	for _, pkg := range binaryPackages {
		source, ok := pkg["Source"]
		if !ok {
			source = pkg["Package"]
		}
		idx := strings.Index(source, " ")
		if idx > -1 {
			source = source[:idx]
		}
		binaryToSource[pkg["Package"]] = source
	}

	installations := make(map[string]float32)

	// Modeled after UDD’s popcon_gatherer.py:
	// https://anonscm.debian.org/cgit/collab-qa/udd.git/tree/udd/popcon_gatherer.py?id=9db1e97eff32691f4df03d1b9ee8a9290a91fc7a
	url := "http://popcon.debian.org/all-popcon-results.txt.gz"
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP Status %s for URL %q, expected OK", resp.Status, url)
	}
	defer resp.Body.Close()
	reader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "Package: ") {
			continue
		}
		// line is e.g. “Package: 0ad                              269  1380   121     4”
		fields := strings.Fields(line)
		// 2016/03/28 16:30:58 fields = ["Package:" "zyne" "16" "14" "3" "0"]
		if len(fields) != 6 {
			log.Printf("Skipping line %q: expected %d fields, got %d\n", fields, 6, len(fields))
			continue
		}
		var insts int64
		for _, field := range fields[2:] {
			converted, err := strconv.ParseInt(field, 0, 64)
			if err != nil {
				log.Printf("Could not convert field %q to integer: %v", field, err)
				continue
			}
			insts += converted
		}
		matches := asciiMatch.FindStringSubmatch(fields[1])
		if len(matches) == 0 {
			if *verbose {
				fmt.Printf("%q is not a valid package name, skipping\n", fields[1])
			}
			continue
		}
		binaryPackage := matches[1]

		sourcePackage, ok := binaryToSource[binaryPackage]
		if !ok {
			if *verbose {
				fmt.Printf("Could not find package %q in binary→source mapping, skipping\n", binaryPackage)
			}
			continue
		}
		installations[sourcePackage] += float32(insts)
	}
	return installations, scanner.Err()
}
