// vim:ts=4:sw=4:noexpandtab
package search

import (
	"fmt"
	"net/url"
	"strings"
)

// Parses the querystring (q= parameter) and moves special tokens such as
// "lang:c" from the querystring into separate arguments.
func RewriteQuery(u url.URL) url.URL {
	// query is a copy which we will modify using Set() and use in the result
	query := u.Query()

	querystr := query.Get("q")
	queryWords := []string{}
	for _, word := range strings.Split(querystr, " ") {
		fmt.Printf("word = %v\n", word)
		lower := strings.ToLower(word)
		if strings.HasPrefix(lower, "filetype:") {
			query.Set("filetype", strings.ToLower(word[len("filetype:"):]))
		} else if strings.HasPrefix(lower, "package:") {
			query.Set("package", word[len("package:"):])
		} else if strings.HasPrefix(lower, "-package:") {
			query.Add("npackage", word[len("-package:"):])
		} else if strings.HasPrefix(lower, "path:") {
			query.Add("path", word[len("path:"):])
		} else if strings.HasPrefix(lower, "-path:") {
			query.Add("npath", word[len("-path:"):])
		} else {
			queryWords = append(queryWords, word)
		}
	}
	query.Set("q", strings.Join(queryWords, " "))
	u.RawQuery = query.Encode()

	return u
}
