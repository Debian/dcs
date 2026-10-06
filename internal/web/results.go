package web

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func startJsonResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	// Set cache time for one hour. The files will ideally get cached both by
	// nginx and the client(s).
	utc := time.Now().UTC()
	cacheSince := utc.Format(http.TimeFormat)
	cacheUntil := utc.Add(1 * time.Hour).Format(http.TimeFormat)
	w.Header().Set("Cache-Control", "max-age=3600, public")
	w.Header().Set("Last-Modified", cacheSince)
	w.Header().Set("Expires", cacheUntil)
}

func (s *queryState) writeResults(page int, results io.Writer, w http.ResponseWriter, r *http.Request) error {
	s.mu.Lock()
	pointers := s.resultPointers
	s.mu.Unlock()
	start := page * resultsPerPage
	if page < 0 || start > pointers.Len() {
		return httpError(http.StatusNotFound, fmt.Errorf("No such page."))
	}
	end := min((page+1)*resultsPerPage, pointers.Len())

	if strings.HasSuffix(r.URL.Path, ".json") {
		startJsonResponse(w)
	}

	chunk, err := pointers.Slice(start, end)
	if err != nil {
		return err
	}
	if err := s.writeFromPointers(results, chunk); err != nil {
		return fmt.Errorf("Could not return results: %v", err)
	}
	return nil
}

func (s *queryState) writePerPkgResults(page int, results io.Writer, w http.ResponseWriter, r *http.Request) error {
	s.mu.Lock()
	packages := s.allPackagesSorted
	bypkg := s.resultPointersByPkg
	s.mu.Unlock()

	start := page * packagesPerPage
	if page < 0 || start > len(packages) {
		return httpError(http.StatusNotFound, fmt.Errorf("No such page."))
	}
	end := min((page+1)*packagesPerPage, len(packages))

	if strings.HasSuffix(r.URL.Path, ".json") {
		startJsonResponse(w)
	}

	results.Write([]byte("["))

	for idx, pkg := range packages[start:end] {
		if idx == 0 {
			fmt.Fprintf(results, `{"Package": "%s", "Results":`, pkg)
		} else {
			fmt.Fprintf(results, `,{"Package": "%s", "Results":`, pkg)
		}
		if err := s.writeFromPointers(results, bypkg[pkg]); err != nil {
			return fmt.Errorf("Could not return results: %v", err)
		}
		results.Write([]byte("}"))
	}
	results.Write([]byte("]"))
	return nil
}

// vim:ts=4:sw=4:noexpandtab
