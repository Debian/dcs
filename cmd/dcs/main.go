// Binary dcs is the swiss-army knife for Debian Code Search. It displays index
// files in a variety of ways.
package main

import (
	"flag"
	"fmt"
	"os"
)

const globalHelp = `dcs - Debian Code Search swiss-army knife

Syntax: dcs [global flags] <command> [flags] [args]

Deployment (shard) commands:
	stats - display stats about this deployment (shard)

Index query commands:
	docids   - list the documents covered by this index
	trigram  - display metadata of the specified trigram
	raw      - print raw (encoded) index data for the specified trigram
	posting  - list the (decoded) posting list for the specified trigram
	matches  - list the filename[:pos] matches for the specified trigram
	search   - list the filename[:pos] matches for the specified search query
	replay   — replay a query log

Index manipulation commands:
	create   - create an index
	merge    - merge multiple index files into one
`

func help(topic string) {
	switch topic {
	case "raw":
		fmt.Fprintf(os.Stdout, "%s", rawHelp)
		raw([]string{"-help"})
	case "trigram":
		fmt.Fprintf(os.Stdout, "%s", trigramHelp)
		trigram([]string{"-help"})
	case "docids":
		fmt.Fprintf(os.Stdout, "%s", docidsHelp)
		docids([]string{"-help"})
	case "posting":
		fmt.Fprintf(os.Stdout, "%s", postingHelp)
		posting([]string{"-help"})
	case "matches":
		fmt.Fprintf(os.Stdout, "%s", matchesHelp)
		matches([]string{"-help"})
	case "create":
		fmt.Fprintf(os.Stdout, "%s", createHelp)
		create([]string{"-help"})
	case "merge":
		fmt.Fprintf(os.Stdout, "%s", mergeHelp)
		merge([]string{"-help"})
	case "search":
		fmt.Fprintf(os.Stdout, "%s", searchHelp)
		search([]string{"-help"})
	case "replay":
		fmt.Fprintf(os.Stdout, "%s", replayHelp)
		replay([]string{"-help"})
	case "":
		flag.Usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown help topic %q", topic)
	}
}

func main() {
	// Global flags (not command-specific)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "%s", globalHelp)
		//fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		return
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "stats":
		stats(args)
	case "raw":
		raw(args)
	case "trigram":
		trigram(args)
	case "docids":
		docids(args)
	case "posting":
		posting(args)
	case "matches":
		matches(args)
	case "create":
		create(args)
	case "merge":
		merge(args)
	case "search":
		search(args)
	case "replay":
		replay(args)
	case "help":
		if len(args) > 0 {
			help(args[0])
		} else {
			help("")
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		flag.Usage()
		os.Exit(1)
	}
}
