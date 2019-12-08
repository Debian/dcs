// vim:ts=4:sw=4:noexpandtab
// These handlers serve server-rendered pages for clients without JavaScript.
// The templates contain a bit of JavaScript that will automatically redirect
// to the more interactive version so that browsers that _do_ have JavaScript
// but follow a link will not end up in the server-rendered version.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Debian/dcs/cmd/dcs-web/common"
	dcsregexp "github.com/Debian/dcs/regexp"
	opentracing "github.com/opentracing/opentracing-go"
)

// XXX: Using a dcsregexp.Match anonymous struct member doesn’t work,
// because we need to assign to the members to get the data from Result
// over into halfRenderedResult.
type halfRenderedResult struct {
	Path          string
	Line          int
	PathRank      float32
	Ranking       float32
	SourcePackage string
	RelativePath  string
	Context       template.HTML
}

func maybeAppendContext(context []string, line string) []string {
	if strings.TrimSpace(line) != "" {
		replaced := line
		for strings.HasPrefix(replaced, "\t") {
			replaced = strings.Replace(replaced, "\t", "    ", 1)
		}
		return append(context, replaced)
	} else {
		return context
	}
}

func splitPath(path string) (sourcePackage string, relativePath string) {
	for i := 0; i < len(path); i++ {
		if path[i] == '_' {
			sourcePackage = path[:i]
			relativePath = path[i:]
			return
		}
	}
	return
}

// NB: Updates to this function must also be performed in static/instant.js.
func updatePagination(currentpage int, resultpages int, baseurl string) string {
	result := `<strong>Pages:</strong> `
	start := currentpage - 5
	if currentpage > 0 && 1 > start {
		start = 1
	}
	if currentpage == 0 && 0 > start {
		start = 0
	}
	end := resultpages
	if currentpage >= 5 && currentpage+5 < end {
		end = currentpage + 5
	}
	if currentpage < 5 && 10 < end {
		end = 10
	}

	if currentpage > 0 {
		result = result + `<a href="` + baseurl + `&page=` + strconv.Itoa(currentpage-1) + `" rel="prev">&lt;</a> `
		result = result + `<a href="` + baseurl + `&page=0">1</a> `
	}

	if start > 1 {
		result = result + `… `
	}

	for i := start; i < end; i++ {
		result = result + `<a style="`
		if i == currentpage {
			result = result + "font-weight: bold"
		}
		result = result + `" href="` + baseurl + `&page=` + strconv.Itoa(i) + `">` + strconv.Itoa(i+1) + `</a> `
	}

	if end < (resultpages - 1) {
		result = result + `… `
	}

	if end < resultpages {
		result = result + `<a href="` + baseurl + `&page=` + strconv.Itoa(resultpages-1) + `">` + strconv.Itoa(resultpages) + `</a>`
	}

	if currentpage < (resultpages - 1) {
		result = result + ` <a href="` + baseurl + `&page=` + strconv.Itoa(currentpage+1) + `" rel="next">&gt;</a> `
	}

	return result
}

func readPackagesFile(queryid string) []string {
	packages := state[queryid].allPackagesSorted
	end := 100
	if end > len(packages) {
		end = len(packages)
	}
	return packages[:end]
}

func renderPerPackage(w http.ResponseWriter, r *http.Request, queryid string, page int) {
	var buffer bytes.Buffer
	if err := writePerPkgResults(queryid, page, &buffer, w, r); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type perPackageResults struct {
		Package    string
		RawResults []dcsregexp.Match `json:"Results"`
		Results    []halfRenderedResult
	}

	var results []perPackageResults
	if err := json.NewDecoder(&buffer).Decode(&results); err != nil {
		http.Error(w,
			fmt.Sprintf("Could not parse results from disk: %v", err),
			http.StatusInternalServerError)
		return
	}

	for idx, pp := range results {
		halfrendered := make([]halfRenderedResult, len(pp.RawResults))
		for idx, result := range pp.RawResults {
			var context []string
			context = maybeAppendContext(context, result.Ctxp2)
			context = maybeAppendContext(context, result.Ctxp1)
			context = append(context, "<strong>"+result.Context+"</strong>")
			context = maybeAppendContext(context, result.Ctxn1)
			context = maybeAppendContext(context, result.Ctxn2)

			sourcePackage, relativePath := splitPath(result.Path)

			halfrendered[idx] = halfRenderedResult{
				Path:          result.Path,
				Line:          result.Line,
				PathRank:      result.PathRank,
				Ranking:       result.Ranking,
				SourcePackage: sourcePackage,
				RelativePath:  relativePath,
				Context:       template.HTML(strings.Join(context, "<br>")),
			}
		}
		results[idx] = perPackageResults{
			Package: pp.Package,
			Results: halfrendered,
		}
	}

	packages := readPackagesFile(queryid)

	basequery := r.URL.Query()
	basequery.Del("page")
	baseurl := r.URL
	baseurl.RawQuery = basequery.Encode()
	pages := int(math.Ceil(float64(len(state[queryid].allPackagesSorted)) / float64(packagesPerPage)))
	pagination := updatePagination(page, pages, baseurl.String())

	basequery.Del("perpkg")
	basequery.Del("q")
	baseurl.RawQuery = basequery.Encode()
	filterurl := baseurl.String()

	if err := common.Templates.ExecuteTemplate(w, "perpackage-results.html", map[string]interface{}{
		"criticalcss": common.CriticalCss,
		"results":     results,
		"filterurl":   filterurl,
		"packages":    packages,
		"pagination":  template.HTML(pagination),
		"q":           r.Form.Get("q"),
		"literal":     r.Form.Get("literal") == "1",
		"page":        page,
		"host":        r.Host,
		"version":     common.Version,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// q= search term
// page= page number
// perpkg= per-package grouping
// literal= literal vs. regex search
func Search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Could not parse form data", http.StatusInternalServerError)
		return
	}

	src := r.RemoteAddr
	query := r.Form.Get("q")
	if query == "" {
		http.Error(w, "Empty query", http.StatusNotFound)
		return
	}
	literal := r.Form.Get("literal")
	if literal == "" {
		literal = "0"
	}

	span := opentracing.SpanFromContext(ctx)
	span.SetOperationName("Serverrendered: " + query)

	// We encode a URL that contains _only_ the q parameter.
	q := url.Values{"q": []string{query}}.Encode() + "&literal=" + literal

	pageStr := r.Form.Get("page")
	if pageStr == "" {
		pageStr = "0"
	}
	page, err := strconv.Atoi(pageStr)
	if err != nil {
		http.Error(w, "Invalid page parameter", http.StatusBadRequest)
		return
	}

	// Uniquely (well, good enough) identify this query for a couple of minutes
	// (as long as we want to cache results). We could try to normalize the
	// query before hashing it, but that seems hardly worth the complexity.
	h := fnv.New64()
	io.WriteString(h, q)
	queryid := fmt.Sprintf("%x", h.Sum64())

	log.Printf("server-render(%q, %q, %q)\n", queryid, src, q)

	if err := validateQuery("?" + q); err != nil {
		log.Printf("[%s] Query %q failed validation: %v\n", src, q, err)
		http.Error(w, fmt.Sprintf("Invalid query: %v", err), http.StatusBadRequest)
		return
	}

	if _, err := maybeStartQuery(ctx, queryid, src, q); err != nil {
		log.Printf("[%s] could not start query: %v\n", src, err)
		http.Error(w, fmt.Sprintf("Could not start query: %v", err), http.StatusInternalServerError)
		return
	}
	if !queryCompleted(queryid) {
		// Prevent caching, as the placeholder is temporary.
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		if err := common.Templates.ExecuteTemplate(w, "placeholder.html", map[string]interface{}{
			"criticalcss": common.CriticalCss,
			"q":           r.Form.Get("q"),
			"literal":     literal,
			"host":        r.Host,
			"version":     common.Version,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		return
	}

	log.Printf("[%s] server-rendering page %d\n", queryid, page)

	if r.Form.Get("perpkg") == "1" {
		renderPerPackage(w, r, queryid, page)
		return
	}

	var buffer bytes.Buffer
	if err := writeResults(queryid, page, &buffer, w, r); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var results []dcsregexp.Match
	if err := json.NewDecoder(&buffer).Decode(&results); err != nil {
		http.Error(w,
			fmt.Sprintf("Could not parse results from disk: %v", err),
			http.StatusInternalServerError)
		return
	}

	halfrendered := make([]halfRenderedResult, len(results))
	for idx, result := range results {
		var context []string
		context = maybeAppendContext(context, result.Ctxp2)
		context = maybeAppendContext(context, result.Ctxp1)
		context = append(context, "<strong>"+result.Context+"</strong>")
		context = maybeAppendContext(context, result.Ctxn1)
		context = maybeAppendContext(context, result.Ctxn2)

		sourcePackage, relativePath := splitPath(result.Path)

		halfrendered[idx] = halfRenderedResult{
			Path:          result.Path,
			Line:          result.Line,
			PathRank:      result.PathRank,
			Ranking:       result.Ranking,
			SourcePackage: sourcePackage,
			RelativePath:  relativePath,
			Context:       template.HTML(strings.Join(context, "<br>")),
		}
	}

	packages := readPackagesFile(queryid)

	basequery := r.URL.Query()
	basequery.Del("page")
	baseurl := r.URL
	baseurl.RawQuery = basequery.Encode()
	pagination := updatePagination(page, state[queryid].resultPages, baseurl.String())

	basequery.Set("perpkg", "1")
	baseurl.RawQuery = basequery.Encode()
	perpkgurl := baseurl.String()

	basequery.Del("perpkg")
	basequery.Del("q")
	baseurl.RawQuery = basequery.Encode()
	filterurl := baseurl.String()

	if err := common.Templates.ExecuteTemplate(w, "results.html", map[string]interface{}{
		"criticalcss": common.CriticalCss,
		"perpkgurl":   perpkgurl,
		"filterurl":   filterurl,
		"results":     halfrendered,
		"packages":    packages,
		"pagination":  template.HTML(pagination),
		"q":           r.Form.Get("q"),
		"literal":     literal == "1",
		"page":        page,
		"host":        r.Host,
		"version":     common.Version,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
