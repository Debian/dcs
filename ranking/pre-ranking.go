// vim:ts=4:sw=4:noexpandtab

// XXX: Unsure whether it actually *should* happen on dcs-web or on the index
// backends. a pro argument for ranking on dcs-web is that we could batch
// queries to the database together and maybe have an advantage that way?

// Pre-ranking happens on dcs-web after the index backends provided their
// results. Without looking at file contents at all, we assign a preliminary
// ranking to each file based on its database entries and whether the query
// string matches parts of the filename.
package ranking

import (
	"database/sql"
	_ "github.com/jbarham/gopgsqldriver"
	"log"
	"path"
)

// Represents an entry from our ranking database (determined by using the
// meta information about source packages).
type StoredRanking struct {
	inst float32
	rdep float32
}

// Consumes a few hundred kilobytes of memory
// ((sizeof(StoredRanking) = 8) * ≈ 17000).
var storedRanking = make(map[string]StoredRanking)

// Open a database connection and read in all the rankings. The amount of
// rankings is in the tens of thousands (currently ≈ 17000) and it saves us
// *a lot* of time when ranking queries which have many possible results (such
// as "smart" with 201043 possible results).
func init() {
	db, err := sql.Open("postgres", "dbname=dcs")
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

// The regular expression trigram index provides us a path to a potential
// result. This data structure represents such a path and allows for ranking
// and sorting each path.
type ResultPath struct {
	Path string
	SourcePkgIdx [2]int
	Ranking float32
}

func (rp *ResultPath) Rank(opts *RankingOpts) {
	// No ranking at all: 807ms
	// query.Match(&rp.Path): 4.96s
	// query.Match(&rp.Path) * query.Match(&sourcePackage): 6.7s
	// full ranking: 24s
	// lookup table: 6.8s

	rp.SourcePkgIdx[1] = 0
	for i := len("/dcs-ssd/unpacked/"); i < len(rp.Path); i++ {
		if rp.Path[i] == '_' {
			rp.SourcePkgIdx[0] = len("/dcs-ssd/unpacked/")
			rp.SourcePkgIdx[1] = i
			break
		}
	}
	if rp.SourcePkgIdx[1] == 0 {
		log.Fatal("Invalid path in result: %s", rp.Path)
	}

	sourcePackage := rp.Path[rp.SourcePkgIdx[0]:rp.SourcePkgIdx[1]]
	//log.Printf("should rank source package %s", sourcePackage)
	ranking := storedRanking[sourcePackage]
	rp.Ranking = 1
	if opts.Inst {
		rp.Ranking *= ranking.inst
	}
	if opts.Rdep {
		rp.Ranking *= ranking.rdep
	}
	if (opts.Filetype || opts.Weighted) && len(opts.Suffixes) > 0 {
		suffix := path.Ext(rp.Path)
		if val, exists := opts.Suffixes[suffix]; exists {
			rp.Ranking *= val
		} else {
			// With a ranking of 0, the result will be thrown away.
			rp.Ranking = 0
		}
	}
	if opts.Weighted {
		rp.Ranking *= 0.4099 * ranking.inst
		rp.Ranking *= 0.3683 * ranking.rdep
	}
}

type ResultPaths []ResultPath

func (r ResultPaths) Len() int {
	return len(r)
}

func (r ResultPaths) Less(i, j int) bool {
	if r[i].Ranking == r[j].Ranking {
		// On a tie, we use the path to make the order of results stable over
		// multiple queries (which can have different results depending on
		// which index backend reacts quicker).
		return r[i].Path > r[j].Path
	}
	return r[i].Ranking > r[j].Ranking
}

func (r ResultPaths) Swap(i, j int) {
	r[i], r[j] = r[j], r[i]
}
