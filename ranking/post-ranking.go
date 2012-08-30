// vim:ts=4:sw=4:noexpandtab

// Post-ranking happens on the source backend (because it has the source files
// in the kernel’s page cache). In the post-ranking phase we can do (limited)
// source file level analysis, such as in which scope the query string was
// matched (comment, top-level, sub-level).
package ranking

import (
	"dcs/regexp"
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

func PostRank(match *regexp.Match) float32 {
	// Ranking: In which scope is the match? The higher the scope, the more
	// important it is.
	scopeRanking := 1.0 - (float32(countSpaces(match.Context)) / 100.0)

	return scopeRanking
}
