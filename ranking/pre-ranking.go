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
	"encoding/json"
	"log"
	"os"
	"path"
	"strings"
)

// Represents an entry from our ranking database (determined by using the
// meta information about source packages).
type StoredRanking struct {
	Inst float32
	Rdep float32
}

// Consumes a few hundred kilobytes of memory
// ((sizeof(StoredRanking) = 8) * ≈ 17000).
var storedRanking = make(map[string]StoredRanking)

// ReadRankingData reads the pre-computed rankings from |path|. It must be
// called before ResultPath.Rank() is called, otherwise Rank() won’t return
// meaningful results.
func ReadRankingData(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(&storedRanking)
}

// The regular expression trigram index provides us a path to a potential
// result. This data structure represents such a path and allows for ranking
// and sorting each path.
type ResultPath struct {
	Path         string
	Position     int
	SourcePkgIdx [2]int
	Ranking      float32
}

func (rp *ResultPath) Rank(opts *RankingOpts) {
	// No ranking at all: 807ms
	// query.Match(&rp.Path): 4.96s
	// query.Match(&rp.Path) * query.Match(&sourcePackage): 6.7s
	// full ranking: 24s
	// lookup table: 6.8s

	rp.SourcePkgIdx[0] = 0
	rp.SourcePkgIdx[1] = 0
	for i := 0; i < len(rp.Path); i++ {
		if rp.Path[i] == '_' {
			rp.SourcePkgIdx[1] = i
			break
		}
	}
	if rp.SourcePkgIdx[1] == 0 {
		log.Fatalf("Invalid path in result: %q", rp.Path)
	}

	sourcePackage := rp.Path[rp.SourcePkgIdx[0]:rp.SourcePkgIdx[1]]
	ranking := storedRanking[sourcePackage]
	rp.Ranking = 1
	if opts.Inst {
		rp.Ranking += ranking.Inst
	}
	if opts.Rdep {
		rp.Ranking += ranking.Rdep
	}
	if (opts.Filetype || opts.Weighted) && len(opts.Suffixes) > 0 {
		suffix := strings.ToLower(path.Ext(rp.Path))
		if val, exists := opts.Suffixes[suffix]; exists {
			rp.Ranking += val
		} else {
			// With a ranking of -1, the result will be thrown away.
			rp.Ranking = -1
			return
		}
	}
	if (opts.Filetype || opts.Weighted) && len(opts.Nsuffixes) > 0 {
		suffix := strings.ToLower(path.Ext(rp.Path))
		if _, exists := opts.Nsuffixes[suffix]; exists {
			rp.Ranking = -1
			return
		}
	}
	if opts.Weighted {
		rp.Ranking += 0.3840 * ranking.Inst
		rp.Ranking += 0.3427 * ranking.Rdep
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
