package web

import (
	"errors"
	"net/http"
)

func (o *Opts) SpeedbumpHandler(w http.ResponseWriter, r *http.Request) {
	// For valid speedbump continuation tokens,
	// start the query and redirect to attach to it.
	q := r.PostFormValue("q")
	token := r.PostFormValue("continuation_token")
	var signed string
	if err := o.speedbumpCookie.Decode("speedbump", token, &signed); err != nil || signed != q {
		// Otherwise (e.g. for expired continuation tokens),
		// redirect to the search handler to start from scratch.
		http.Redirect(w, r, "/search?"+q, http.StatusSeeOther)
		return
	}

	cl := clientFromRequest(r, "/speedbump", "")
	if _, _, err := o.maybeStartQuery(r.Context(), queryID(q), cl, q); err != nil {
		if errors.Is(err, errTooManyQueries) {
			http.Error(w, err.Error(), http.StatusTooManyRequests)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/search?"+q+"&exhaustive=1", http.StatusSeeOther)
}
