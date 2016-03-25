// vim:ts=4:sw=4:noexpandtab
package search

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	start = regexp.MustCompile(`(?i)^\s*(-?(?:filetype|package|pkg|path|file)):(\S+)\s+`)
	end   = regexp.MustCompile(`(?i)\s+(-?(?:filetype|package|pkg|path|file)):(\S+)\s*$`)
)

func rewriteFilters(query url.Values, filtersRe *regexp.Regexp) url.Values {
	qstr := query.Get("q")
	// Using a regexp so the start/end indices of each match are easily
	// available for subsequent slicing of the query string.  The performance
	// (regexp @ 0.011 ms vs. string @ 0.004 ms) is minimal enough that the
	// extra hit for regexp is acceptable, given the simpler code.
	matches := filtersRe.FindStringSubmatch(qstr)
	for matches != nil {
		// matches is [entire_match, filter_name, filter_value]
		filter := strings.ToLower(matches[1])
		value := matches[2]

		filter = strings.Replace(filter, "pkg", "package", 1)
		if filter == "-file" {
			filter = "npath"
		} else if strings.HasPrefix(filter, "-") {
			filter = "n" + filter[1:]
		}
		if strings.HasSuffix(filter, "filetype") {
			value = strings.ToLower(value)
		}
		query.Add(filter, value)

		if filtersRe == start {
			qstr = strings.TrimPrefix(qstr, matches[0])
		} else {
			qstr = strings.TrimSuffix(qstr, matches[0])
		}
		matches = filtersRe.FindStringSubmatch(qstr)
	}

	query.Set("q", qstr)
	return query
}

// Parses the querystring (q= parameter) and moves special tokens such as
// "lang:c" from the querystring into separate arguments.
func RewriteQuery(u url.URL) url.URL {
	// query is a copy which we will modify using Set() and use in the result
	query := rewriteFilters(u.Query(), start)
	query = rewriteFilters(query, end)

	u.RawQuery = query.Encode()

	return u
}
