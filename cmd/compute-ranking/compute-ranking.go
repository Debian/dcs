// vim:ts=4:sw=4:noexpandtab
package main

import (
	"compress/bzip2"
	"database/sql"
	"flag"
	"fmt"
	_ "github.com/lib/pq"
	"github.com/mstap/godebiancontrol"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var mirrorPath = flag.String("mirrorPath",
	"/media/sdd1/debian-source-mirror/",
	"Path to the debian source mirror (which contains the 'dists' and 'pool' folder)")
var dryRun = flag.Bool("dry_run", false, "Don’t actually write anything to the database.")
var verbose = flag.Bool("verbose", false, "Print ranking information about every package")
var popconInstSrc map[string]float32 = make(map[string]float32)

// Fills popconInstSrc from the Ultimate Debian Database (udd). The popcon
// installation count is stored normalized by dividing through the total amount
// of popcon installations.
func fillPopconInst() {
	db, err := sql.Open("postgres", "dbname=udd host=/var/run/postgresql/ sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}

	var totalInstallations int
	var packageName string
	var installations int
	err = db.QueryRow("SELECT SUM(insts) FROM popcon").Scan(&totalInstallations)
	if err != nil {
		log.Fatalf("Could not get SUM(insts) FROM popcon: %v", err)
	}

	if *verbose {
		log.Printf("total %d installations", totalInstallations)
	}

	rows, err := db.Query("SELECT source, insts FROM popcon_src")
	if err != nil {
		log.Fatal(err)
	}
	for rows.Next() {
		if err = rows.Scan(&packageName, &installations); err != nil {
			log.Fatal(err)
		}

		// XXX: We multiply 1000 here because all values are < 0.0009.
		popconInstSrc[packageName] = (float32(installations) / float32(totalInstallations)) * 1000
	}

}

func mustLoadMirroredControlFile(name string) []godebiancontrol.Paragraph {
	file, err := os.Open(filepath.Join(*mirrorPath, "dists/sid/main/", name))
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	contents, err := godebiancontrol.Parse(bzip2.NewReader(file))
	if err != nil {
		log.Fatal(err)
	}

	return contents
}

func main() {
	flag.Parse()

	fillPopconInst()

	db, err := sql.Open("postgres", "dbname=dcs host=/var/run/postgresql/ sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}

	insert, err := db.Prepare("INSERT INTO pkg_ranking (package, popcon, rdepends) VALUES ($1, $2, $3)")
	if err != nil {
		log.Fatal(err)
	}
	defer insert.Close()

	update, err := db.Prepare("UPDATE pkg_ranking SET popcon = $2, rdepends = $3 WHERE package = $1")
	if err != nil {
		log.Fatal(err)
	}
	defer update.Close()

	sourcePackages := mustLoadMirroredControlFile("source/Sources.bz2")
	binaryPackages := mustLoadMirroredControlFile("binary-amd64/Packages.bz2")

	reverseDeps := make(map[string]uint)
	for _, pkg := range binaryPackages {
		// We need to filter duplicates, because consider this:
		// agda-bin Recommends: libghc-agda-dev (>= 2.3.2), libghc-agda-dev (<< 2.3.2)
		dependsOn := make(map[string]bool)
		// NB: This differs from what apt-cache rdepends spit out. apt-cache
		// also considers the Replaces field.
		allDeps := pkg["Depends"] + "," + pkg["Suggests"] + "," + pkg["Recommends"] + "," + pkg["Enhances"]
		for _, dep := range strings.FieldsFunc(allDeps, func(r rune) bool {
			return r == ',' || r == '|'
		}) {
			trimmed := strings.TrimSpace(dep)
			spaceIdx := strings.Index(trimmed, " ")
			if spaceIdx == -1 {
				spaceIdx = len(trimmed)
			}
			dependsOn[trimmed[:spaceIdx]] = true
		}
		for name, _ := range dependsOn {
			reverseDeps[name] += 1
		}
	}

	for _, pkg := range sourcePackages {
		rdepcount := float32(0)
		for _, packageName := range strings.Split(pkg["Binary"], ",") {
			packageName = strings.TrimSpace(packageName)
			if packageName == "" {
				continue
			}
			rdepcount += float32(reverseDeps[packageName])
		}
		srcpkg := pkg["Package"]
		packageRank := popconInstSrc[srcpkg]
		rdepcount = 1.0 - (1.0 / float32(rdepcount+1))
		if *verbose {
			fmt.Printf("%f %f %s\n", packageRank, rdepcount, srcpkg)
		}
		if *dryRun {
			continue
		}
		if _, err = insert.Exec(srcpkg, packageRank, rdepcount); err != nil {
			if _, err = update.Exec(srcpkg, packageRank, rdepcount); err != nil {
				log.Fatal(err)
			}
		}
	}
}
