// Binary dcs is the swiss-army knife for Debian Code Search. It displays index
// files in a variety of ways.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"

	"net/http"
	_ "net/http/pprof"
)

const globalHelp = `dcs - Debian Code Search swiss-army knife

Syntax: dcs [global flags] <command> [flags] [args]

To get help on any command, use dcs <command> -help or dcs help <command>.

Index query commands:
	du       — shows disk usage of the specified index files
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

func usage(fset *flag.FlagSet, help string) func() {
	return func() {
		fmt.Fprintf(fset.Output(), "%s", help)
		fmt.Fprintf(fset.Output(), "\nFlags:\n")
		fset.PrintDefaults()
	}
}

// Global flags (not command-specific)
var cpuprofile, memprofile, listen, traceFn string

func init() {
	// TODO: remove in favor of running as a test
	flag.StringVar(&cpuprofile, "cpuprofile", "", "")
	flag.StringVar(&memprofile, "memprofile", "", "write memory profile to this file")
	flag.StringVar(&listen, "listen", "", "speak HTTP on this [host]:port if non-empty")
	flag.StringVar(&traceFn, "trace", "", "create runtime/trace file")
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "%s", globalHelp)
		fmt.Fprintf(flag.CommandLine.Output(), "\nGlobal flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if listen != "" {
		go func() {
			if err := http.ListenAndServe(listen, nil); err != nil {
				log.Fatal(err)
			}
		}()
	}

	if cpuprofile != "" {
		f, err := os.Create(cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if memprofile != "" {
		defer func() {
			f, err := os.Create(memprofile)
			if err != nil {
				log.Fatal(err)
			}
			runtime.GC()
			pprof.WriteHeapProfile(f)
			f.Close()
		}()
	}

	if traceFn != "" {
		f, err := os.Create(traceFn)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		if err := trace.Start(f); err != nil {
			log.Fatal(err)
		}
		defer trace.Stop()
	}

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		return
	}
	cmd, args := args[0], args[1:]
	if cmd == "help" {
		if len(args) > 0 {
			cmd = args[0]
		} else {
			cmd = ""
		}
		args = []string{"-help"}
	}
	var err error
	switch cmd {
	case "du":
		err = du(args)
	case "raw":
		err = raw(args)
	case "trigram":
		err = trigram(args)
	case "docids":
		err = docids(args)
	case "posting":
		err = posting(args)
	case "matches":
		err = matches(args)
	case "create":
		err = create(args)
	case "merge":
		err = merge(args)
	case "search":
		err = search(args)
	case "replay":
		err = replay(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		flag.Usage()
		os.Exit(1)
	}
	if err != nil {
		log.Fatal(err)
	}
}
