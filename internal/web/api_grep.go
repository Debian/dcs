package web

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Debian/dcs/internal/httperr"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"
)

const grepUsage = `usage: /grep?q=search+term
  q=        what to search for: a literal string, URL-encoded
  regexp=1  search q as a regular expression (RE2 syntax)
  limit=N   print the N best matches (default: 25)
keyword filters inside q:
  package:REGEXP  or -package to negate
  path:REGEXP     or -path to negate
  filetype:c      or go, python, …
examples:
  /grep?q=load_font(+package:i3-wm
  /grep?regexp=1&q=%28load%7Cset%29_font
notes:
  Literal search is case-sensitive.
  For case-insensitive search, use e.g. ?regexp=1&q=(?i)term
  Files are searched line by line,
  meaning ^ and $ match line start and line end.
`

type grepRequest struct {
	q       string
	literal bool
	limit   int
}

func synonymFor0(s string) bool {
	return s == "0" || s == "false" || s == "no" || s == "off"
}

func synonymFor1(s string) bool {
	return s == "1" || s == "true" || s == "yes" || s == "on"
}

func validateParameters(r *http.Request) (*grepRequest, error) {
	req := &grepRequest{
		q:       r.FormValue("q"),
		literal: true, // default to literal mode
		limit:   25,
	}
	if req.q == "" {
		return nil, errors.New("no search term specified")
	}

	if limitStr := r.FormValue("limit"); limitStr != "" {
		var err error
		req.limit, err = strconv.Atoi(limitStr)
		if err != nil || req.limit < 1 || req.limit > 1000 {
			return nil, errors.New("invalid limit parameter")
		}
	}

	if v := strings.ToLower(r.FormValue("literal")); v != "" {
		if synonymFor0(v) {
			req.literal = false // regexp mode
		} else if synonymFor1(v) {
			// literal mode (default)
		} else {
			return nil, errors.New("invalid literal parameter")
		}
	}
	// For ergonomics, treat the regexp parameter as the inverse of literal.
	if v := strings.ToLower(r.FormValue("regexp")); v != "" {
		if synonymFor1(v) {
			req.literal = false // regexp mode
		} else if synonymFor0(v) {
			// literal mode (default)
		} else {
			return nil, errors.New("invalid regexp parameter")
		}
	}

	// Return an error on unknown parameters
	// instead of silently accepting them
	// to provide guardrails to users (and their agents).
	for param := range r.URL.Query() {
		switch param {
		case "q",
			"limit",
			"literal",
			"regexp":
			// Valid query parameters, handled above.
		default:
			return nil, fmt.Errorf("unexpected query parameter %q", param)
		}
	}

	return req, nil
}

func (a *apiserver) grep(w http.ResponseWriter, r *http.Request) (err error) {
	ctx := r.Context()

	key, err := a.decoder.Decode(r.Header.Get("x-dcs-apikey"))
	if err != nil {
		return httperr.Error(http.StatusForbidden, errors.New("invalid x-dcs-apikey header"))
	}

	cl := clientFromRequest(r, "/grep", key.Subject)
	srcLabel := prometheus.Labels{"source": key.Subject}
	defer func() {
		if err != nil && ctx.Err() == nil {
			metricErroredQueries.With(srcLabel).Inc()
		}
	}()

	req, err := validateParameters(r)
	if err != nil {
		return httperr.Error(http.StatusBadRequest, fmt.Errorf("%v. %s", err, grepUsage))
	}

	// We encode a URL that contains _only_ the q (and literal) parameter.
	literal := "1"
	if !req.literal {
		literal = "0"
	}
	q := url.Values{"q": []string{req.q}}.Encode() + "&literal=" + literal

	s, err := a.runAndWaitFor(ctx, cl, srcLabel, q)
	if err != nil {
		return err
	}

	s.mu.Lock()
	filesTotal := 0
	for _, total := range s.filesTotal {
		filesTotal += total
	}
	w.Header().Set("X-Codesearch-FilesTotal", strconv.Itoa(filesTotal))
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err := writeGrepResults(w, s, req.limit); err != nil {
		return err
	}

	return nil
}

func writeGrepResults(w http.ResponseWriter, s *queryState, limit int) error {
	perBackend, err := perBackendFromState(s)
	if err != nil {
		return err
	}
	defer func() {
		for _, mapping := range perBackend {
			mapping.Close()
		}
	}()
	s.mu.Lock()
	resultPointers := s.resultPointers
	var totalReadBytes uint64
	for _, n := range s.readBytes {
		totalReadBytes += n
	}
	readGB := float64(totalReadBytes) / 1024 / 1024 / 1024
	seconds := (s.ended.Sub(s.started) - s.queued).Seconds()
	status := fmt.Sprintf("# %d results in %d packages (searched %.2f GB in %.2fs)",
		s.numResults, len(s.allPackagesSorted), readGB, seconds)
	more := "" // ends up in the footer
	if s.numResults > limit {
		status += fmt.Sprintf("; showing %d", limit)
		more = "# For more results, add e.g. &limit=100 to the URL.\n"
	}
	s.mu.Unlock()
	bw := bufio.NewWriter(w)
	fmt.Fprintln(bw, status)
	var msg sourcebackendpb.SearchReply
	example := "" // path of the best match
	for ptr, err := range resultPointers.All() {
		if err != nil {
			return err
		}
		mapping := perBackend[ptr.backendidx]
		if err := proto.Unmarshal(mapping.Data[ptr.offset:ptr.offset+int64(ptr.length)], &msg); err != nil {
			return err
		}
		if msg.Type != sourcebackendpb.SearchReply_MATCH {
			continue
		}
		if limit == 0 {
			break
		}
		limit--
		m := msg.Match
		if example == "" {
			example = m.Path
		}
		_, err := fmt.Fprintf(bw, "%s:%d: %s\n", m.Path, m.Line, m.Context)
		if err != nil {
			return err
		}
	}
	// Include a footer after the search results,
	// so that follow-up requests are obvious.
	if more != "" {
		bw.WriteString(more)
	}
	bw.WriteString("# To narrow, add package:REGEXP, path:REGEXP or filetype:c to q.\n")
	bw.WriteString("# (Prefix - to negate, e.g. -package:linux)\n")
	if example != "" {
		fmt.Fprintf(bw, "# To read a file: /show/%s\n", example)
	}
	return bw.Flush()
}
