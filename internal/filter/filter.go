package filter

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// TODO: filter /debian/api/ (in every linux package), e.g.
// linux_3.14.4-1/debian/abi/3.14-1/armel_none_orion5x
var (
	ignoredDirnamesList = flag.String("ignored_dirnames",
		".pc,.git,libtool.m4",
		"(comma-separated list of) names of directories that will be deleted from packages when importing")

	// NB: we don’t skip "configure" since that might be a custom shell-script
	// NB: we actually skip some autotools files because they blow up our index otherwise
	ignoredFilenamesList = flag.String("ignored_filenames",
		"NEWS,COPYING,LICENSE,CHANGES,Makefile.in,ltmain.sh,config.guess,config.sub,depcomp,aclocal.m4,libtool.m4,.gitignore,57710_init_values.c,57711_init_values.c,57712_init_values.c",
		"(comma-separated list of) names of files that will be deleted from packages when importing")

	ignoredSuffixesList = flag.String("ignored_suffixes",
		"dic,xml,xsl,sgml,symbols",
		"(comma-separated list of) suffixes of files that will be deleted from packages when importing")

	onlySmallFilesSuffixesList = flag.String("only_small_files_suffixes",
		"ref,result,S,out,rst,def,afm,ps,pao,tom,ovp,UPF,map,ucm,json,svg,ppd,acc,ipp,eps,sym,pass,F90,tei,stl,tmp,dmp,vtk,csv,stp,decTest,test,lla,pamphlet,html",
		"(comma-separated list of) suffixes of files that will not be indexed if their size is more than 64 KB")

	ignoredDirnames        = make(map[string]bool)
	ignoredFilenames       = make(map[string]bool)
	ignoredSuffixes        = make(map[string]bool)
	onlySmallFilesSuffixes = make(map[string]bool)

	errIgnoredDirnames        = errors.New("file listed in -ignored_dirnames")
	errIgnoredFilenames       = errors.New("file listed in -ignored_filenames")
	errIgnoredSuffixes        = errors.New("file listed in -ignored_suffixes")
	errOnlySmallFilesSuffixes = errors.New("file listed in -only_small_files_suffixes but larger than 64 KB")
	errTooLarge               = errors.New("file larger than 1GiB")
	errManpageSuffix          = errors.New("file seems to be a man page as per its suffix")
)

func Init() {
	for _, entry := range strings.Split(*ignoredDirnamesList, ",") {
		ignoredDirnames[entry] = true
	}
	for _, entry := range strings.Split(*ignoredFilenamesList, ",") {
		ignoredFilenames[entry] = true
	}
	for _, entry := range strings.Split(*ignoredSuffixesList, ",") {
		ignoredSuffixes[entry] = true
	}
	for _, entry := range strings.Split(*onlySmallFilesSuffixesList, ",") {
		onlySmallFilesSuffixes[entry] = true
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
func Ignored(info os.FileInfo, dir, filename string) error {
	// Some filenames (e.g.
	// "xblast-tnt-levels_20050106-2/reconstruct\xeeon2.xal") contain
	// invalid UTF-8 and will break when sending them via JSON later
	// on. Filter those out early to avoid breakage.
	if path := filepath.Join(dir, filename); !utf8.ValidString(path) {
		return fmt.Errorf("path %q is not valid UTF-8", path)
	}

	if info.IsDir() {
		if ignoredDirnames[filename] {
			return errIgnoredDirnames
		}
		return nil
	}
	size := info.Size()
	// index/write.go will skip the file if it’s too big, so we might as
	// well skip it here and save the disk space.
	if size > (1 << 30) {
		return errTooLarge
	}

	// TODO: peek inside the files (we’d have to read them anyways) and
	// check for messages that indicate that the file is generated. either
	// by autoconf or by bison for example.
	if ignoredFilenames[filename] {
		return errIgnoredFilenames
	}

	if hasManpageSuffix(filename) {
		return errManpageSuffix
	}
	idx := strings.LastIndex(filename, ".")
	if idx > -1 {
		if ignoredSuffixes[filename[idx+1:]] &&
			!strings.HasPrefix(strings.ToLower(filename), "cmakelists.txt") {
			return errIgnoredSuffixes
		}
		if size > 65*1024 && onlySmallFilesSuffixes[filename[idx+1:]] {
			return errOnlySmallFilesSuffixes
		}
	}

	return nil
}
