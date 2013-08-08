// vim:ts=4:sw=4:noexpandtab
package search

import (
	"net/url"
	"testing"
)

func rewrite(t *testing.T, urlstr string) url.URL {
	baseQuery, err := url.Parse(urlstr)
	if err != nil {
		t.Fatal(err)
	}

	return RewriteQuery(*baseQuery)
}

func TestRewriteQuery(t *testing.T) {
	// Verify that RewriteQuery() doesn’t break simple search terms
	rewritten := rewrite(t, "/search?q=searchterm")
	querystr := rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %s, got %s", "searchterm", querystr)
	}

	// Verify that the filetype: keyword is properly moved
	rewritten = rewrite(t, "/search?q=searchterm+filetype%3Ac")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %s, got %s", "searchterm", querystr)
	}
	filetype := rewritten.Query().Get("filetype")
	if filetype != "c" {
		t.Fatalf("Expected filetype %s, got %s", "c", filetype)
	}

	// Verify that the filetype: keyword is treated case-insensitively
	rewritten = rewrite(t, "/search?q=searchterm+filetype%3AC")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %s, got %s", "searchterm", querystr)
	}
	filetype = rewritten.Query().Get("filetype")
	if filetype != "c" {
		t.Fatalf("Expected filetype %s, got %s", "c", filetype)
	}

	// Verify that the package: keyword is recognized (case-sensitively)
	rewritten = rewrite(t, "/search?q=searchterm+package%3Ai3-WM")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %s, got %s", "searchterm", querystr)
	}
	pkg := rewritten.Query().Get("package")
	if pkg != "i3-WM" {
		t.Fatalf("Expected package %s, got %s", "i3-WM", pkg)
	}

	// Verify that the -package: (negative) keyword is recognized (case-sensitively)
	rewritten = rewrite(t, "/search?q=searchterm+-package%3Ai3-WM")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %s, got %s", "searchterm", querystr)
	}
	pkg = rewritten.Query().Get("npackage")
	if pkg != "i3-WM" {
		t.Fatalf("Expected npackage %s, got %s", "i3-WM", pkg)
	}

	// Verify that the multiple keywords work as expected
	rewritten = rewrite(t, "/search?q=searchterm+package%3Ai3-WM+filetype%3Ac")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %s, got %s", "searchterm", querystr)
	}
	pkg = rewritten.Query().Get("package")
	if pkg != "i3-WM" {
		t.Fatalf("Expected package %s, got %s", "i3-WM", pkg)
	}
	filetype = rewritten.Query().Get("filetype")
	if filetype != "c" {
		t.Fatalf("Expected filetype %s, got %s", "c", filetype)
	}

	// Verify that accessing the map for a keyword that doesn't exist doesn't cause iterations
	rewritten = rewrite(t, "/search?q=searchterm+package%3Ai3-WM")
	vmap := rewritten.Query()["some_array"]
	for _, v := range vmap {
		t.Fatalf("Unexpected value in some_array '%s'", v)
	}

	// Verify that multiple values for some of the keywords can be passed
	rewritten = rewrite(t, "/search?q=searchterm+-package%3Ai3-WM+-package%3Afoo")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %s, got %s", "searchterm", querystr)
	}
	vmap = rewritten.Query()["npackage"]
	seen := 0
	for _, v := range vmap {
		seen++
		if v != "i3-WM" && v != "foo" {
			t.Fatalf("Unexpected value for -package keyword, got '%s'", v)
		}
	}
	if seen != 2 {
		t.Fatalf("Expected two elements in the hash of the -package keyword, saw %d", seen)
	}
}
