// vim:ts=4:sw=4:noexpandtab

package ranking

import (
	"net/url"
	"strconv"
)

type RankingOpts struct {
	// pre-ranking

	// pre-ranking: amount of reverse dependencies
	Rdep bool

	// pre-ranking: popcon installation count
	Inst bool

	// pre-ranking: does the search query match the path?
	Pathmatch bool

	// pre-ranking: does the search query match the source package name?
	Sourcepkgmatch bool

	// post-ranking

	// post-ranking: in which scope is the match?
	Scope bool

	// post-ranking: does the search query (with enforced word boundaries)
	// match the line?
	Linematch bool

	// meta: turns on all rankings and uses 'optimal' weights (as determined in
	// the thesis).
	Weighted bool
}

// TODO: parse floats which specify the weight of each ranking
func boolFromQuery(query url.Values, name string) bool {
	intval, err := strconv.ParseInt(query.Get(name), 10, 8)
	if err != nil {
		return false
	}

	return intval == 1
}

func RankingOptsFromQuery(query url.Values) RankingOpts {
	var result RankingOpts
	result.Rdep = boolFromQuery(query, "rdep")
	result.Inst = boolFromQuery(query, "inst")
	result.Pathmatch = boolFromQuery(query, "pathmatch")
	result.Sourcepkgmatch = boolFromQuery(query, "sourcepkgmatch")
	result.Scope = boolFromQuery(query, "scope")
	result.Linematch = boolFromQuery(query, "linematch")
	result.Weighted = boolFromQuery(query, "weighted")
	return result
}
