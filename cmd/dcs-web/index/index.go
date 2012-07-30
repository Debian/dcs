// vim:ts=4:sw=4:noexpandtab
package index

import (
	"fmt"
	"net/http"
)

func Index(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `<html><form action="/search" method="get"><input type="text" name="q"><input type="submit">`)
}
