package main

import (
	"flag"
	"os"
	"strings"
)

var (
	ignoredDirnamesList = flag.String("ignored_dirnames",
		".pc,po,.git,libtool.m4",
		"(comma-separated list of) names of directories that will be deleted from packages when importing")

	// NB: we don’t skip "configure" since that might be a custom shell-script
	// NB: we actually skip some autotools files because they blow up our index otherwise
	ignoredFilenamesList = flag.String("ignored_filenames",
		"NEWS,COPYING,LICENSE,CHANGES,Makefile.in,ltmain.sh,config.guess,config.sub,depcomp,aclocal.m4,libtool.m4,.gitignore",
		"(comma-separated list of) names of files that will be deleted from packages when importing")

	ignoredSuffixesList = flag.String("ignored_suffixes",
		"conf,dic,cfg,man,xml,xsl,html,sgml,pod,po,txt,tex,rtf,docbook,symbols",
		"(comma-separated list of) suffixes of files that will be deleted from packages when importing")

	ignoredDirnames  = make(map[string]bool)
	ignoredFilenames = make(map[string]bool)
	ignoredSuffixes  = make(map[string]bool)
)

func setupFilters() {
	for _, entry := range strings.Split(*ignoredDirnamesList, ",") {
		ignoredDirnames[entry] = true
	}
	for _, entry := range strings.Split(*ignoredFilenamesList, ",") {
		ignoredFilenames[entry] = true
	}
	for _, entry := range strings.Split(*ignoredSuffixesList, ",") {
		ignoredSuffixes[entry] = true
	}
}

// Returns true when the file matches .[0-9]$ (cheaper than a regular
// expression).
func hasManpageSuffix(filename string) bool {
	return len(filename) > 2 &&
		filename[len(filename)-2] == '.' &&
		filename[len(filename)-1] >= '0' &&
		filename[len(filename)-1] <= '9'
}

// Returns true for files that should not be indexed for various reasons:
// • generated files
// • non-source (but text) files, e.g. .doc, .svg, …
func ignored(info os.FileInfo, dir, filename string) bool {
	if info.IsDir() {
		if ignoredDirnames[filename] {
			return true
		}
	} else {
		// TODO: peek inside the files (we’d have to read them anyways) and
		// check for messages that indicate that the file is generated. either
		// by autoconf or by bison for example.
		if ignoredFilenames[filename] ||
			// Don’t match /debian/changelog or /debian/README, but
			// exclude changelog and readme files generally.
			(!strings.HasSuffix(dir, "/debian/") &&
				strings.HasPrefix(strings.ToLower(filename), "changelog") ||
				strings.HasPrefix(strings.ToLower(filename), "readme")) ||
			hasManpageSuffix(filename) {
			return true
		}
		idx := strings.LastIndex(filename, ".")
		if idx > -1 {
			if ignoredSuffixes[filename[idx+1:]] {
				return true
			}
		}
	}

	return false
}
