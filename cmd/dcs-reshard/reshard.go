// Command dcs-reshard generates a shell script that relocates per-package
// index and source data between shards after a change to the sharding
// function, without re-downloading the Debian archive.
//
// It is written for the "shard by source package name, not name_version"
// change: it reads the packages currently on disk under
// <root>/shardN/idx/<name>_<version>, computes the shard each one should live
// on with the production shardmapping.TaskIdxForPackage over the *name only*,
// and emits `cp -al` (source) + `mv` (index) commands for every package that
// moves. Sources are hard-linked rather than moved so the old shard's live
// index keeps finding its files until the new indexes are swapped in; the old
// copies are recorded in a delete list for removal once that has happened.
//
// The tool is read-only against <root>: it only lists directory names and
// prints a script to stdout (summary to stderr). Review the script, then run
// it as root on the dcs host while dcs-feeder is stopped and no merge is in
// flight.
//
// It cuts the name at the first '_' itself before hashing, so it produces the
// correct name-only target whether or not the name-only change has already
// landed in internal/shardmapping.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Debian/dcs/internal/shardmapping"
)

var (
	root = flag.String("root",
		"/srv/dcs",
		"directory containing the shardN subdirectories")
	shardsFlag = flag.Int("shards",
		0,
		"expected number of shards; 0 auto-detects from <root>/shardN directories. When non-zero, the tool aborts if the detected count differs.")
	deleteList = flag.String("delete_list",
		"/srv/dcs/reshard-delete.list",
		"path (on the target host) the generated script records moved-away source directories into, for deletion after the new indexes are live")
)

var shardDirRe = regexp.MustCompile(`^shard([0-9]+)$`)

// detectShards returns the number of contiguous shardN directories (shard0 …
// shardN-1) found directly under root.
func detectShards(root string) (int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, err
	}
	seen := make(map[int]bool)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m := shardDirRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, err
		}
		seen[n] = true
	}
	if len(seen) == 0 {
		return 0, fmt.Errorf("no shardN directories found in %s", root)
	}
	for i := 0; i < len(seen); i++ {
		if !seen[i] {
			return 0, fmt.Errorf("shard directories are not contiguous: shard%d missing (found %d shard dirs)", i, len(seen))
		}
	}
	return len(seen), nil
}

// sourceName returns the source package name for a <name>_<version> index
// directory name. Debian source package names cannot contain '_', so the first
// '_' separates name from version.
func sourceName(pkg string) string {
	name, _, _ := strings.Cut(pkg, "_")
	return name
}

type move struct {
	pkg      string
	from, to int
}

func run() error {
	flag.Parse()
	if flag.NArg() > 0 {
		// Go's flag package stops at the first non-flag argument (or after
		// "--"), so e.g. "dcs-reshard -- -root=/x" would silently scan the
		// default -root. Refuse instead of planning moves for the wrong tree.
		return fmt.Errorf("unexpected arguments %q (flags after a positional argument or \"--\" are not parsed)", flag.Args())
	}

	nshards, err := detectShards(*root)
	if err != nil {
		return err
	}
	if *shardsFlag != 0 && *shardsFlag != nshards {
		return fmt.Errorf("-shards=%d but detected %d shardN directories in %s", *shardsFlag, nshards, *root)
	}

	before := make([]int, nshards)
	after := make([]int, nshards)
	var moves []move
	var scanned int

	for from := 0; from < nshards; from++ {
		idxDir := filepath.Join(*root, fmt.Sprintf("shard%d", from), "idx")
		entries, err := os.ReadDir(idxDir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			pkg := e.Name()
			// Skip in-flight merge temporaries (e.g. full.<ts>.tmp, *.tmp).
			if strings.HasSuffix(pkg, ".tmp") {
				continue
			}
			if !strings.Contains(pkg, "_") {
				// Not a <name>_<version> package directory.
				fmt.Fprintf(os.Stderr, "WARN: ignoring unexpected entry shard%d/idx/%s\n", from, pkg)
				continue
			}
			scanned++
			before[from]++
			to := shardmapping.TaskIdxForPackage(sourceName(pkg), nshards)
			after[to]++
			if to != from {
				moves = append(moves, move{pkg: pkg, from: from, to: to})
			}
		}
	}

	sort.Slice(moves, func(i, j int) bool {
		if moves[i].from != moves[j].from {
			return moves[i].from < moves[j].from
		}
		return moves[i].pkg < moves[j].pkg
	})

	emitScript(os.Stdout, *root, *deleteList, moves)
	emitSummary(os.Stderr, nshards, scanned, before, after, moves)
	return nil
}

// moveFunc is the shell function the generated script calls once per moving
// package. It is safe to re-run after an interruption at any point:
//
//   - The source is hard-linked into a temporary directory and only renamed
//     into place once cp -al has finished, so an existing target source is
//     always complete and never gets a nested <pkg>/<pkg> copy on re-run.
//   - A package whose index has already moved is skipped; one whose index has
//     disappeared since the script was generated (garbage-collected) is
//     skipped instead of aborting the script under set -e.
//   - The delete list is only ever appended to, so a re-run never loses the
//     old source paths recorded by an earlier, interrupted run.
const moveFunc = `move() { # from_shard to_shard pkg
	from=$1; to=$2; pkg=$3
	fromidx="$ROOT/shard$from/idx/$pkg"; toidx="$ROOT/shard$to/idx/$pkg"
	fromsrc="$ROOT/shard$from/src/$pkg"; tosrc="$ROOT/shard$to/src/$pkg"
	if [ -e "$toidx" ] && [ -e "$fromidx" ]; then
		echo "ERROR $pkg: index present on both shard$from and shard$to" >&2
		exit 1
	fi
	if [ -e "$toidx" ]; then
		echo "SKIP $pkg: already moved to shard$to" >&2
		return 0
	fi
	if [ ! -e "$fromidx" ]; then
		echo "SKIP $pkg: no longer in shard$from/idx (garbage-collected?)" >&2
		return 0
	fi
	if [ -d "$fromsrc" ]; then
		if [ ! -e "$tosrc" ]; then
			rm -rf "$tosrc.reshard-tmp"
			cp -al "$fromsrc" "$tosrc.reshard-tmp"
			mv "$tosrc.reshard-tmp" "$tosrc"
		fi
		echo "$fromsrc" >> "$DELETE_LIST"
	else
		echo "WARN $pkg: no src on shard$from, moving idx only" >&2
	fi
	mv "$fromidx" "$toidx"
}
`

func emitScript(w io.Writer, root, deleteList string, moves []move) {
	fmt.Fprintf(w, "#!/bin/sh\n")
	fmt.Fprintf(w, "# Generated by dcs-reshard on %s.\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(w, "# Relocates %d packages to the shard chosen by shardmapping.TaskIdxForPackage\n", len(moves))
	fmt.Fprintf(w, "# over the source package name only.\n")
	fmt.Fprintf(w, "#\n")
	fmt.Fprintf(w, "# PRECONDITIONS: run as root on the dcs host, with dcs-feeder and all\n")
	fmt.Fprintf(w, "# dcs-package-importer instances stopped. Sources are hard-linked (cp -al),\n")
	fmt.Fprintf(w, "# so <root> must be a single filesystem. Safe to re-run after an\n")
	fmt.Fprintf(w, "# interruption. After the new indexes are live, delete the old source\n")
	fmt.Fprintf(w, "# copies recorded in the delete list.\n")
	fmt.Fprintf(w, "set -eu\n\n")
	fmt.Fprintf(w, "ROOT=%s\n", shellQuote(root))
	fmt.Fprintf(w, "DELETE_LIST=%s\n", shellQuote(deleteList))
	// Append-only: never truncate, so a re-run keeps earlier entries.
	fmt.Fprintf(w, "touch \"$DELETE_LIST\"\n\n")
	fmt.Fprint(w, moveFunc)
	fmt.Fprintf(w, "\n")

	for _, m := range moves {
		fmt.Fprintf(w, "move %d %d %s\n", m.from, m.to, shellQuote(m.pkg))
	}

	fmt.Fprintf(w, "\nsort -u -o \"$DELETE_LIST\" \"$DELETE_LIST\"\n")
	fmt.Fprintf(w, "echo \"dcs-reshard: processed %d packages.\" >&2\n", len(moves))
	fmt.Fprintf(w, "echo \"Once the new indexes are live (+>=10min), delete old sources with:\" >&2\n")
	fmt.Fprintf(w, "echo \"  xargs -d '\\\\n' rm -rf < $DELETE_LIST\" >&2\n")
}

func emitSummary(w *os.File, nshards, scanned int, before, after []int, moves []move) {
	fmt.Fprintf(w, "shards:   %d\n", nshards)
	fmt.Fprintf(w, "scanned:  %d packages\n", scanned)
	fmt.Fprintf(w, "moving:   %d packages\n\n", len(moves))
	fmt.Fprintf(w, "shard  pkgs before  pkgs after\n")
	for i := 0; i < nshards; i++ {
		fmt.Fprintf(w, "%-5d  %11d  %10d\n", i, before[i], after[i])
	}
}

// shellQuote single-quotes s for POSIX sh. Package names and versions never
// contain single quotes, but quote defensively anyway.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "dcs-reshard: %v\n", err)
		os.Exit(1)
	}
}
