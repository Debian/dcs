// vim:ts=4:sw=4:noexpandtab
package ranking

import (
	"fmt"
	"regexp"
)

// Represents a query string with pre-compiled regular expressions for faster
// matching. 
type QueryStr struct {
	query string
	boundaryRegexp *regexp.Regexp
	anywhereRegexp *regexp.Regexp
}

func NewQueryStr(query string) QueryStr {
	var result QueryStr
	result.query = query
	// XXX: This only works for very simple (one-word) queries.
	quotedQuery := regexp.QuoteMeta(query)
	fmt.Printf("quoted query: %s\n", quotedQuery)
	result.boundaryRegexp = regexp.MustCompile(`\b` + quotedQuery + `\b`)
	result.anywhereRegexp = regexp.MustCompile(quotedQuery)
	return result
}

func (qs *QueryStr) Match(path *string) float32 {
	// XXX: These values might need to be tweaked.
	if qs.boundaryRegexp.MatchString(*path) {
		return 1.0
	} else if qs.anywhereRegexp.MatchString(*path) {
		return 0.75
	}

	return 0.5
}
