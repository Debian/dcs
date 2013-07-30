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
	pkg := rewritten.Query().Get("npackage")
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
}
