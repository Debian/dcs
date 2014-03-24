// +build !no_ranking_db

package ranking

import (
	"database/sql"
	_ "github.com/lib/pq"
	"log"
)

func ReadDB(storedRanking map[string]StoredRanking) {
	db, err := sql.Open("postgres", "dbname=dcs host=/var/run/postgresql/ sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}

	rankQuery, err := db.Prepare("SELECT package, popcon, rdepends FROM pkg_ranking")
	if err != nil {
		log.Fatal(err)
	}

	rows, err := rankQuery.Query()
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	var inst, rdep float32
	var pkg string
	for rows.Next() {
		if err = rows.Scan(&pkg, &inst, &rdep); err != nil {
			log.Fatal(err)
		}
		storedRanking[pkg] = StoredRanking{inst, rdep}
	}
}
