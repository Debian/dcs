package web

import (
	"bufio"
	"cmp"
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
  q=            what to search for: a literal string, URL-encoded
  regexp=1      search q as a regular expression (RE2 syntax)
  limit=N       print the N best matches (default: 25)
  context=2     print 2 (or 1, default: 0) context lines
  perpackage=1  print the 2 best matches of the best N packages

keyword filters inside q:
  package:REGEXP  name without version (e.g. ^i3-wm$), -package to negate
  path:REGEXP     path within the package (e.g. ^debian/), -path to negate
  filetype:c      or go, python, …

examples:
  /grep?q=load_font(+package:i3-wm
  /grep?regexp=1&q=%28load%7Cset%29_font
  To read a file, use /show/ with a path from the results,
  e.g. /show/i3-wm_4.25.1-1/libi3/font.c

notes:
  Literal search is case-sensitive.
  For case-insensitive search, use e.g. ?regexp=1&q=(?i)term
  Files are searched line by line,
  meaning ^ and $ match line start and line end.
`

type grepRequest struct {
	q          string
	literal    bool
	limit      int
	context    int // default 0, else 1 or 2 (max)
	perPackage bool
}

func synonymFor0(s string) bool {
	return s == "0" || s == "false" || s == "no" || s == "off"
}

func synonymFor1(s string) bool {
	return s == "1" || s == "true" || s == "yes" || s == "on"
}

func validateParameters(r *http.Request) (*grepRequest, error) {
	req := &grepRequest{
		q:          r.FormValue("q"),
		literal:    true, // default to literal mode
		limit:      25,
		perPackage: synonymFor1(r.FormValue("perpackage")),
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

	if c := r.FormValue("context"); c != "" {
		n, err := strconv.Atoi(c)
		if err != nil || n < 0 || n > 2 {
			return nil, errors.New("invalid context parameter (supported: 0, 1 or 2 lines of context)")
		}
		req.context = n
	}

	// Return an error on unknown parameters
	// instead of silently accepting them
	// to provide guardrails to users (and their agents).
	for param := range r.URL.Query() {
		switch param {
		case "q",
			"perpackage",
			"limit",
			"literal",
			"regexp",
			"context":
			// Valid query parameters, handled above.

		case "continuation_token":
			// Validated in runAndWaitFor.

		default:
			return nil, fmt.Errorf("unexpected query parameter %q", param)
		}
	}

	return req, nil
}

func (a *apiserver) grep(w http.ResponseWriter, r *http.Request) (err error) {
	ctx := r.Context()

	var subject string
	if apikey := r.Header.Get("x-dcs-apikey"); apikey != "" {
		key, err := a.decoder.Decode(apikey)
		if err != nil {
			return httperr.Error(http.StatusForbidden, errors.New("invalid x-dcs-apikey header"))
		}
		subject = key.Subject
	}

	cl := clientFromRequest(r, "/grep", subject)
	srcLabel := prometheus.Labels{"source": cmp.Or(subject, "anonymous")}
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

	s, err := a.runAndWaitFor(ctx, cl, srcLabel, q, r.FormValue("continuation_token"))
	if err != nil {
		return err
	}

	s.mu.Lock()
	candidatesTotal := 0
	for _, total := range s.filesTotal {
		candidatesTotal += total
	}
	w.Header().Set("X-Codesearch-Candidates", strconv.Itoa(candidatesTotal))
	w.Header().Set("X-Codesearch-Results", strconv.Itoa(s.numResults))
	w.Header().Set("X-Codesearch-Packages", strconv.Itoa(len(s.allPackagesSorted)))
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err := writeGrepResults(w, r, s, req); err != nil {
		return err
	}

	return nil
}

func writeGrepResults(w http.ResponseWriter, r *http.Request, s *queryState, req *grepRequest) error {
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
	if req.perPackage {
		pkgs := s.allPackagesSorted
		if len(pkgs) > req.limit {
			status += fmt.Sprintf("; showing %d packages", req.limit)
			more = "# For more packages, add e.g. &limit=100 to the URL.\n"
			pkgs = pkgs[:req.limit]
		}
		var byPkg memPointers
		for _, pkg := range pkgs {
			byPkg = append(byPkg, s.resultPointersByPkg[pkg]...)
		}
		resultPointers = byPkg
		// Turn req.limit from the package limit into a result limit
		// for the loop below (otherwise we lose results).
		req.limit = len(byPkg)
	} else if s.numResults > req.limit {
		status += fmt.Sprintf("; showing %d", req.limit)
		more = "# For more results, add e.g. &limit=100 to the URL.\n"
	}
	if s.speedbump != nil {
		status += "; incomplete (see footer)"
		more += "# This query was expensive! Did you mean to run it fully?\n"
		more += fmt.Sprintf("# Reason: %s\n", s.speedbump.Message)
		more += fmt.Sprintf("# To search all files: /grep?%s&continuation_token=%s\n", r.URL.RawQuery, url.QueryEscape(s.speedbump.ContinuationToken))
	}

	s.mu.Unlock()
	bw := bufio.NewWriter(w)
	fmt.Fprintln(bw, status)
	var msg sourcebackendpb.SearchReply
	example := "" // path of the best match
	var lastfile string
	var lastpkg string
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
		if req.limit == 0 {
			break
		}
		req.limit--
		m := msg.Match
		if example == "" {
			example = m.Path
		}
		if req.perPackage {
			if pkg, _, _ := strings.Cut(m.Package, "_"); pkg != lastpkg {
				var results int
				for _, bstate := range s.perBackend {
					results += bstate.allPackages[m.Package]
				}
				if _, err := fmt.Fprintf(bw, "\n# package %s (results: %d)\n", pkg, results); err != nil {
					return err
				}
				lastpkg = pkg
			}
		}
		if req.context == 0 {
			// No context requested: just print the line and we are done
			if _, err := fmt.Fprintf(bw, "%s:%d: %s\n", m.Path, m.Line, m.Context); err != nil {
				return err
			}
			continue
		}
		// Long output format (with context lines) requested.
		if lastfile == m.Path {
			if _, err := fmt.Fprintf(bw, "--\n"); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(bw, "\n%s\n", m.Path); err != nil {
				return err
			}
		}
		lastfile = m.Path
		if req.context >= 2 && m.Ctxp2 != "" {
			if _, err := fmt.Fprintf(bw, "%d- %s\n", m.Line-2, m.Ctxp2); err != nil {
				return err
			}
		}
		if req.context >= 1 && m.Ctxp1 != "" {
			if _, err := fmt.Fprintf(bw, "%d- %s\n", m.Line-1, m.Ctxp1); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintf(bw, "%d: %s\n", m.Line, m.Context)
		if err != nil {
			return err
		}
		if req.context >= 1 && m.Ctxn1 != "" {
			if _, err := fmt.Fprintf(bw, "%d- %s\n", m.Line+1, m.Ctxn1); err != nil {
				return err
			}
		}
		if req.context >= 2 && m.Ctxn2 != "" {
			if _, err := fmt.Fprintf(bw, "%d- %s\n", m.Line+2, m.Ctxn2); err != nil {
				return err
			}
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
