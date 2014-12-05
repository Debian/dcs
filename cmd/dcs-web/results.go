package main

import (
	"fmt"
	"io"
	"math"
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

func writeResults(queryid string, page int, results io.Writer, w http.ResponseWriter, r *http.Request) error {
	pointers := state[queryid].resultPointers
	pages := int(math.Ceil(float64(len(pointers)) / float64(resultsPerPage)))
	if page > pages {
		http.Error(w, "No such page.", http.StatusNotFound)
		return nil
	}
	start := page * resultsPerPage
	end := (page + 1) * resultsPerPage
	if end > len(pointers) {
		end = len(pointers)
	}

	if strings.HasSuffix(r.URL.Path, ".json") {
		startJsonResponse(w)
	}

	if err := writeFromPointers(queryid, results, pointers[start:end]); err != nil {
		return fmt.Errorf("Could not return results: %v", err)
	}
	return nil
}

func writePerPkgResults(queryid string, page int, results io.Writer, w http.ResponseWriter, r *http.Request) error {
	bypkg := state[queryid].resultPointersByPkg
	packages := state[queryid].allPackagesSorted

	pages := int(math.Ceil(float64(len(packages)) / float64(packagesPerPage)))
	if page > pages {
		http.Error(w, "No such page.", http.StatusNotFound)
		return nil
	}
	start := page * packagesPerPage
	end := (page + 1) * packagesPerPage
	if end > len(packages) {
		end = len(packages)
	}

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
		if err := writeFromPointers(queryid, results, bypkg[pkg]); err != nil {
			return fmt.Errorf("Could not return results: %v", err)
		}
		results.Write([]byte("}"))
	}
	results.Write([]byte("]"))
	return nil
}

// vim:ts=4:sw=4:noexpandtab
