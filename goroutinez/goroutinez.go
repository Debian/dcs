// Exports runtime variables in a machine-readable format for monitoring.
package goroutinez

import (
	"fmt"
	"net/http"
	"runtime"
)

func Goroutinez(w http.ResponseWriter, r *http.Request) {
	buf := make([]byte, 4096)
	for {
		if n := runtime.Stack(buf, true); n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, len(buf)*2)
	}

	fmt.Fprintf(w, "%s\n", string(buf))
}

// vim:ts=4:sw=4:noexpandtab
