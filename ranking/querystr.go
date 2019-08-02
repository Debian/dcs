// vim:ts=4:sw=4:noexpandtab
package ranking

import (
	"regexp"
	"strings"
)

// Represents a query string with pre-compiled regular expressions for faster
// matching.
type QueryStr struct {
	query          string
	boundaryRegexp *regexp.Regexp
	anywhereRegexp *regexp.Regexp
}

func NewQueryStr(query string) QueryStr {
	var result QueryStr
	result.query = query
	// Remove (?i) at the beginning of the query, which (at least currently)
	// means case-insensitive.
	strippedQuery := strings.Replace(query, "(?i)", "", 1)
	// XXX: This only works for very simple (one-word) queries.
	quotedQuery := regexp.QuoteMeta(strippedQuery)
	//fmt.Printf("quoted query: %s\n", quotedQuery)
	result.boundaryRegexp = regexp.MustCompile(`(?i)\b` + quotedQuery + `\b`)
	result.anywhereRegexp = regexp.MustCompile(`(?i)` + quotedQuery)
	return result
}

func (qs *QueryStr) Match(path *string) float32 {
	// XXX: These values might need to be tweaked.

	index := qs.boundaryRegexp.FindStringIndex(*path)
	if index != nil {
		return 0.75 + (0.25 * (1.0 - float32(index[0])/float32(len(*path))))
	}

	index = qs.anywhereRegexp.FindStringIndex(*path)
	if index != nil {
		return 0.5 + (0.25 * (1.0 - float32(index[0])/float32(len(*path))))
	}

	return 0.5
}
