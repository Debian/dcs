// vim:ts=4:sw=4:noexpandtab
package ranking

import (
	"regexp/syntax"
	"strings"
)

// Represents a query string which can quickly be matched
// against a file path or match context line.
type QueryStr struct {
	query string
}

func NewQueryStr(query string) QueryStr {
	return QueryStr{
		// XXX: This only works for very simple (one-word) queries.
		// More complex queries like hash.*rational cannot match
		// and therefore do not get any ranking-based boost.
		query: strings.Replace(query, "(?i)", "", 1),
	}
}

// indexBoundary checks if qs.query matches path (case-insensitively),
// surrounded by word boundaries, or in regexp: (?i)\bquery\b
func (qs *QueryStr) indexBoundary(path string) int {
	for i := 0; i+len(qs.query) <= len(path); i++ {
		end := i + len(qs.query)
		if strings.EqualFold(path[i:end], qs.query) &&
			isWordBoundary(path, i) && isWordBoundary(path, end) {
			return i
		}
	}
	return -1
}

func isWordBoundary(s string, i int) bool {
	wordBefore := isWordCharAt(s, i-1)
	wordAfter := isWordCharAt(s, i)
	// A word boundary (\b) means a non-word is followed by a word,
	// or a word is followed by a non-word:
	startOfWord := !wordBefore && wordAfter
	endOfWord := wordBefore && !wordAfter
	return startOfWord || endOfWord
}

func isWordCharAt(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false // out of bounds
	}
	return syntax.IsWordChar(rune(s[i]))
}

func (qs *QueryStr) indexAnywhere(path string) int {
	// Not using strings.ToLower to always avoid allocations.
	for i := 0; i+len(qs.query) <= len(path); i++ {
		if strings.EqualFold(path[i:i+len(qs.query)], qs.query) {
			return i
		}
	}
	return -1
}

func (qs *QueryStr) Match(path *string) float32 {
	// XXX: These values might need to be tweaked.

	index := qs.indexBoundary(*path)
	if index > -1 {
		return 0.75 + (0.25 * (1.0 - float32(index)/float32(len(*path))))
	}

	index = qs.indexAnywhere(*path)
	if index > -1 {
		return 0.5 + (0.25 * (1.0 - float32(index)/float32(len(*path))))
	}

	return 0.5
}
