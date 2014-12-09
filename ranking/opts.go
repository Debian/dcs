// vim:ts=4:sw=4:noexpandtab

package ranking

import (
	"net/url"
	"strconv"
)

type RankingOpts struct {
	// Map of file suffix (e.g. ".c") ranking. This is filled in based on the
	// filetype= parameter (which is extracted from the query string).
	Suffixes map[string]float32
	// Same thing, but for the nfiletype parameter (from the -filetype:
	// keywords in the query).
	Nsuffixes map[string]float32

	// pre-ranking

	// pre-ranking: amount of reverse dependencies
	Rdep bool

	// pre-ranking: popcon installation count
	Inst bool

	// pre-ranking: filetype
	Filetype bool

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

func addSuffixesForFiletype(suffixes *map[string]float32, filetype string) {
	switch filetype {
	case "c":
		(*suffixes)[".c"] = 0.75
		(*suffixes)[".h"] = 0.75
	case "c++":
		(*suffixes)[".cpp"] = 0.75
		(*suffixes)[".cxx"] = 0.75
		(*suffixes)[".hpp"] = 0.75
		(*suffixes)[".hxx"] = 0.75
		(*suffixes)[".h"] = 0.75
		// Some people write C++ in .c files
		(*suffixes)[".c"] = 0.55
	case "perl":
		// perl scripts
		(*suffixes)[".pl"] = 0.75
		// perl modules
		(*suffixes)[".pm"] = 0.75
		// test-cases, preferred because they usually make for good examples
		(*suffixes)[".t"] = 0.80
	case "php":
		(*suffixes)[".php"] = 0.75
	case "python":
		(*suffixes)[".py"] = 0.75
	case "go":
		fallthrough
	case "golang":
		(*suffixes)[".go"] = 0.75
	case "java":
		(*suffixes)[".java"] = 0.75
	case "ruby":
		(*suffixes)[".rb"] = 0.75
	case "shell":
		(*suffixes)[".sh"] = 0.75
		(*suffixes)[".bash"] = 0.75
		(*suffixes)[".zsh"] = 0.75
	case "vala":
		(*suffixes)[".vala"] = 0.75
		(*suffixes)[".vapi"] = 0.75
	case "erlang":
		(*suffixes)[".erl"] = 0.75
	case "js":
		fallthrough
	case "javascript":
		(*suffixes)[".js"] = 0.75
	case "json":
		(*suffixes)[".json"] = 0.75
	}
}

func RankingOptsFromQuery(query url.Values) RankingOpts {
	var result RankingOpts
	result.Suffixes = make(map[string]float32)
	result.Nsuffixes = make(map[string]float32)
	types := query["filetype"]
	for _, t := range types {
		addSuffixesForFiletype(&result.Suffixes, t)
	}
	excludetypes := query["nfiletype"]
	for _, t := range excludetypes {
		addSuffixesForFiletype(&result.Nsuffixes, t)
	}
	result.Rdep = boolFromQuery(query, "rdep")
	result.Inst = boolFromQuery(query, "inst")
	result.Filetype = boolFromQuery(query, "filetype")
	result.Pathmatch = boolFromQuery(query, "pathmatch")
	result.Sourcepkgmatch = boolFromQuery(query, "sourcepkgmatch")
	result.Scope = boolFromQuery(query, "scope")
	result.Linematch = boolFromQuery(query, "linematch")
	// Special case: weighted is the default, so assume true if unset.
	if _, ok := query["weighted"]; !ok {
		result.Weighted = true
	} else {
		result.Weighted = boolFromQuery(query, "weighted")
	}
	return result
}
