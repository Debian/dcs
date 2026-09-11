package web

import (
	"context"
	"net/http"
)

type httpErr struct {
	code int
	err  error
}

func (h *httpErr) Error() string {
	return h.err.Error()
}

func httpError(code int, err error) error {
	return &httpErr{code, err}
}

func handleError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	if err == context.Canceled {
		return // client canceled the request
	}
	code := http.StatusInternalServerError
	unwrapped := err
	if he, ok := err.(*httpErr); ok {
		code = he.code
		unwrapped = he.err
	}
	http.Error(w, unwrapped.Error(), code)
}

func errorHandler(h func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		handleError(w, err)
	})
}
