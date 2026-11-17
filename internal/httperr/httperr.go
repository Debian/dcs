package httperr

import (
	"log"
	"net/http"
	"strings"
)

type httpErr struct {
	code int
	err  error
}

func (h *httpErr) Error() string {
	return h.err.Error()
}

func Error(code int, err error) error {
	return &httpErr{code, err}
}

func handleError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	if r.Context().Err() != nil {
		return // client canceled the request
	}
	code := http.StatusInternalServerError
	unwrapped := err
	if he, ok := err.(*httpErr); ok {
		code = he.code
		unwrapped = he.err
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Del("Expires")
	// The first line contains the error,
	// all following lines are usage text.
	// We log only the first line.
	firstLine, _, _ := strings.Cut(unwrapped.Error(), "\n")
	log.Printf("%s: HTTP %d %s", r.URL.Path, code, firstLine)
	http.Error(w, unwrapped.Error(), code)
}

func ErrorHandler(h func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		handleError(w, r, err)
	})
}
