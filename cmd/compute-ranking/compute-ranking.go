// vim:ts=4:sw=4:noexpandtab
package main

import (
	"database/sql"
	_ "github.com/jbarham/gopgsqldriver"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"regexp"
)

var packageLine *regexp.Regexp = regexp.MustCompile(`^Package: (.+)`)
var binaryLine *regexp.Regexp = regexp.MustCompile(`^Binary: (.+)`)
var numShards *int = flag.Int("shards", 1, "Number of index shards (the index will be split into 'shard' different files)")
var popconInst map[string]float32 = make(map[string]float32)

// Fills popconInst from the Ultimate Debian Database (udd). The popcon
// installation count is stored normalized by dividing through the total amount
// of popcon installations.
func fillPopconInst() {
	db, err := sql.Open("postgres", "dbname=udd")
	if err != nil {
		log.Fatal(err)
	}

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

func main() {
	flag.Parse()
	fmt.Println("Debian Code Search ranking tool")

	// TODO: make the path configurable
	mirrorPath := "/media/sdg/debian-source-mirror/"

	fillPopconInst()

	db, err := sql.Open("postgres", "dbname=dcs")
	if err != nil {
		log.Fatal(err)
	}

	insert, err := db.Prepare("INSERT INTO pkg_ranking (package, popcon) VALUES ($1, $2)")
	if err != nil {
		log.Fatal(err)
	}
	defer insert.Close()

	update, err := db.Prepare("UPDATE pkg_ranking SET popcon = $2 WHERE package = $1")
	if err != nil {
		log.Fatal(err)
	}
	defer update.Close()

	// Walk through all source packages
	file, err := os.Open(mirrorPath + "/dists/sid/main/source/Sources")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	content, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatal(err)
	}

	lastSourceName := ""
	for _, line := range strings.Split(string(content), "\n") {
		if m := packageLine.FindStringSubmatch(line); len(m) == 2 {
			lastSourceName = m[1]
			continue
		}

		if m := binaryLine.FindStringSubmatch(line); len(m) == 2 {
			packageRank := float32(0)
			binaryPackages := strings.Split(m[1], ",")
			for _, packageName := range binaryPackages {
				packageName = strings.TrimSpace(packageName)
				if popconRank, ok := popconInst[packageName]; ok {
					if popconRank > packageRank {
						packageRank = popconRank
					}
				}
			}
			fmt.Printf("%f %s\n", packageRank, lastSourceName)
			if _, err = insert.Exec(lastSourceName, packageRank); err != nil {
				if _, err = update.Exec(lastSourceName, packageRank); err != nil {
					log.Fatal(err)
				}
			}
		}
	}
}
