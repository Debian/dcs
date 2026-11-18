// vim:ts=4:sw=4:noexpandtab

package ranking

import "maps"

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

var ByFiletype = map[string]map[string]float32{
	"c": {
		".c": 0.75,
		".h": 0.75,
	},
	"objc": {
		".m": 0.75,
		".h": 0.75,
		".c": 0.75,
	},
	"objc++": {
		".mm":  0.75,
		".cc":  0.75,
		".cpp": 0.75,
		".h":   0.75,
	},
	"c++": {
		".cc":  0.75,
		".cpp": 0.75,
		".cxx": 0.75,
		".hpp": 0.75,
		".hxx": 0.75,
		".h":   0.75,
		".hh":  0.75,
		// Some people write C++ in .c files
		".c": 0.55,
	},
	"perl": {
		// test-cases, preferred because they usually make for good examples
		".t": 0.80,
		// perl scripts
		".pl": 0.75,
		// perl modules
		".pm": 0.75,
	},
	"php": {
		".php": 0.75,
	},
	"python": {
		".py": 0.75,
	},
	"go": {
		".go": 0.75,
	},
	"golang": {
		".go": 0.75,
	},
	"java": {
		".java": 0.75,
	},
	"kotlin": {
		".kt": 0.75,
	},
	"ruby": {
		".rb": 0.75,
	},
	"shell": {
		".sh":   0.75,
		".bash": 0.75,
		".zsh":  0.75,
	},
	"vala": {
		".vala": 0.75,
		".vapi": 0.75,
	},
	"erlang": {
		".erl": 0.75,
	},
	"js": {
		".js": 0.75,
	},
	"javascript": {
		".js": 0.75,
	},
	"typescript": {
		".ts":  0.75,
		".tsx": 0.75, // TypeScript + JSX (React)
	},
	"json": {
		".json": 0.75,
	},
	"lisp": {
		".lisp": 0.75,
		".el":   0.75, // Emacs Lisp
		".scm":  0.75, // Scheme
	},
	"rust": {
		".rs": 0.75,
	},
	"zig": {
		".zig": 0.75,
	},
	"haskell": {
		".hs": 0.75,
	},
	"ocaml": {
		".ml": 0.75,
	},
	"markdown": {
		".md":       0.75,
		".markdown": 0.75,
	},
}

func RankingOptsFromFiletypes(filetypes, nfiletypes []string) RankingOpts {
	result := RankingOpts{
		Weighted:  true, // default
		Suffixes:  make(map[string]float32),
		Nsuffixes: make(map[string]float32),
	}
	for _, t := range filetypes {
		maps.Copy(result.Suffixes, ByFiletype[t])
	}
	for _, t := range nfiletypes {
		maps.Copy(result.Nsuffixes, ByFiletype[t])
	}
	return result
}
