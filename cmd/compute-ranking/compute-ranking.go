// vim:ts=4:sw=4:noexpandtab
package main

import (
	"bytes"
	"database/sql"
	"flag"
	"fmt"
	_ "github.com/jbarham/gopgsqldriver"
	"github.com/mstap/godebiancontrol"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var packageLine *regexp.Regexp = regexp.MustCompile(`^Package: (.+)`)
var binaryLine *regexp.Regexp = regexp.MustCompile(`^Binary: (.+)`)
var udeb *regexp.Regexp = regexp.MustCompile(`\budeb\b`)
var mirrorPath = flag.String("mirrorPath",
	"/media/sdd1/debian-source-mirror/",
	"Path to the debian source mirror (which contains the 'dists' and 'pool' folder)")
var dryRun = flag.Bool("dry_run", false, "Don’t actually write anything to the database.")
var popconInst map[string]float32 = make(map[string]float32)
var popconInstSrc map[string]float32 = make(map[string]float32)

// Fills popconInst from the Ultimate Debian Database (udd). The popcon
// installation count is stored normalized by dividing through the total amount
// of popcon installations.
func fillPopconInst() {
	db, err := sql.Open("postgres", "dbname=udd")
	if err != nil {
		log.Fatal(err)
	}

	// XXX: Using popcon_src would make this code a lot easier. See if
	// refactoring it is useful in the future.
	rows, err := db.Query("SELECT SUM(insts) FROM popcon")
	if err != nil {
		log.Fatal(err)
	}

	if !rows.Next() {
		log.Fatal("rows.Next() failed")
	}
	var totalInstallations int
	if err = rows.Scan(&totalInstallations); err != nil {
		log.Fatal("rows.Scan() failed")
	}
	rows.Close()

	log.Printf("total %d installations", totalInstallations)

	rows, err = db.Query("SELECT package, insts FROM popcon WHERE package != '_submissions'")
	if err != nil {
		log.Fatal(err)
	}
	for rows.Next() {
		var packageName string
		var installations int

		if err = rows.Scan(&packageName, &installations); err != nil {
			log.Fatal(err)
		}

		// XXX: We multiply 1000 here because all values are < 0.0009.
		popconInst[packageName] = (float32(installations) / float32(totalInstallations)) * 1000
	}
}

func countReverseDepends(out string) int {
	packages := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "  ") {
			continue
		}

		parts := strings.Split(line, ":")
		packages[parts[0]] = true
	}

	return len(packages)
}

func main() {
	flag.Parse()
	fmt.Println("Debian Code Search ranking tool")

	fillPopconInst()

	db, err := sql.Open("postgres", "dbname=dcs")
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

	// Walk through all source packages
	file, err := os.Open(filepath.Join(*mirrorPath, "dists/sid/main/source/Sources"))
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	sourcePackages, err := godebiancontrol.Parse(file)
	if err != nil {
		log.Fatal(err)
	}

	udebs := make(map[string]bool)
	for _, pkg := range sourcePackages {
		// Fill our map of udebs. Used later on to avoid running apt-cache
		// rdepends on udebs.
		packageList := strings.Split(pkg["Package-List"], "\n")
		for _, pkg := range packageList {
			pkg = strings.TrimSpace(pkg)
			parts := strings.Split(pkg, " ")
			if (len(parts) > 1 && parts[1] == "udeb") ||
				(len(parts) > 2 && parts[2] == "debian-installer") ||
				(len(parts) > 0 && strings.HasSuffix(parts[0], "-udeb")) {
				udebs[parts[0]] = true
				fmt.Printf("%s is an UDEB\n", parts[0])
			}
		}
		packageRank := float32(0)
		rdepcount := float32(0)
		binaryPackages := strings.Split(pkg["Binary"], ",")
		for _, packageName := range binaryPackages {
			packageName = strings.TrimSpace(packageName)
			if udebs[packageName] {
				fmt.Printf("skipping %s because it’s not a deb\n", packageName)
				continue
			}
			if packageName == "" {
				continue
			}
			if popconRank, ok := popconInst[packageName]; ok {
				if popconRank > packageRank {
					packageRank = popconRank
				}
			}
			var out bytes.Buffer
			cmd := exec.Command("apt-cache", "rdepends", packageName)
			cmd.Stdout = &out
			if err = cmd.Run(); err != nil {
				log.Printf("ERROR: %v\n", err)
			}
			rdepcount += float32(countReverseDepends(out.String()))
		}
		rdepcount = 1.0 - (1.0 / float32(rdepcount+1))
		fmt.Printf("%f %d %s\n", packageRank, rdepcount, pkg["Package"])
		if *dryRun {
			continue
		}
		if _, err = insert.Exec(pkg["Package"], packageRank, rdepcount); err != nil {
			if _, err = update.Exec(pkg["Package"], packageRank, rdepcount); err != nil {
				log.Fatal(err)
			}
		}
	}
}
