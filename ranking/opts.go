// vim:ts=4:sw=4:noexpandtab

package ranking

import (
	"net/url"
	"strconv"
)

type RankingOpts struct {
	Rdep, Inst, Pathmatch, Sourcepkgmatch bool
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
	return result
}


