// vim:ts=4:sw=4:noexpandtab

// Post-ranking happens on the source backend (because it has the source files
// in the kernel’s page cache). In the post-ranking phase we can do (limited)
// source file level analysis, such as in which scope the query string was
// matched (comment, top-level, sub-level).
package ranking

import (
	"github.com/Debian/dcs/regexp"
	"unicode"
)

//var packageLocation *regexp.Regexp = regexp.MustCompile(`debian-source-mirror/unpacked/([^/]+)_`)

func countSpaces(line string) int32 {
	spaces := int32(0)
	for _, r := range line {
		if !unicode.IsSpace(r) {
			break
		}
		spaces += 1
	}
	return spaces
}

func PostRank(opts RankingOpts, match *regexp.Match, querystr *QueryStr) float32 {
	totalRanking := float32(1)

	line := match.Context

	if opts.Scope || opts.Weighted {
		// Ranking: In which scope is the match? The higher the scope, the more
		// important it is.
		scopeRanking := 1.0 - (float32(countSpaces(line)) / 100.0)
		totalRanking *= scopeRanking
	}

	if opts.Linematch || opts.Weighted {
		// Ranking: Does the search query (with enforced word boundaries) match the
		// line? If yes, earlier matches are better (such as function names versus
		// parameter types).
		index := querystr.boundaryRegexp.FindStringIndex(line)
		if index != nil {
			matchRanking := 0.75 + (0.25 * (1.0 - float32(index[0]) / float32(len(line))))
			totalRanking *= matchRanking
		} else {
			// Punish the lines in which there was no word boundary match.
			totalRanking *= 0.5
		}
	}

	return totalRanking
}
