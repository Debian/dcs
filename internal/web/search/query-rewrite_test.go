// vim:ts=4:sw=4:noexpandtab
package search

import (
	"net/url"
	"testing"
)

func rewrite(t *testing.T, urlstr string) url.URL {
	t.Helper()

	baseQuery, err := url.Parse(urlstr)
	if err != nil {
		t.Fatal(err)
	}

	return RewriteQuery(*baseQuery)
}

func BenchmarkRewriteQuery(b *testing.B) {
	for i := 0; i < b.N; i++ {
		baseQuery, err := url.Parse("/search?q=searchterm+package%3Ai3-WM+filetype%3Ac")
		if err != nil {
			b.Fatal(err)
		}

		RewriteQuery(*baseQuery)
	}
}

func TestRewriteQuery(t *testing.T) {
	// Verify that RewriteQuery() doesn’t break simple search terms
	rewritten := rewrite(t, "/search?q=searchterm")
	querystr := rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %q, got %q", "searchterm", querystr)
	}

	// Verify that the amount of whitespace between filters and search terms aren't relevant
	rewritten = rewrite(t, "/search?q=package:foo++searchterm")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %q, got %q", "searchterm", querystr)
	}

	// Verify that the amount of in a search term is relevant
	rewritten = rewrite(t, "/search?q=package:foo+search++term")
	querystr = rewritten.Query().Get("q")
	if querystr != "search  term" {
		t.Fatalf("Expected search query %q, got %q", "search term", querystr)
	}

	// Verify that the filetype: keyword is properly moved
	rewritten = rewrite(t, "/search?q=searchterm+filetype%3Ac")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %q, got %q", "searchterm", querystr)
	}
	filetype := rewritten.Query().Get("filetype")
	if filetype != "c" {
		t.Fatalf("Expected filetype %q, got %q", "c", filetype)
	}

	// Verify that the filetype: keyword is treated case-insensitively
	rewritten = rewrite(t, "/search?q=searchterm+filetype%3AC")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %q, got %q", "searchterm", querystr)
	}
	filetype = rewritten.Query().Get("filetype")
	if filetype != "c" {
		t.Fatalf("Expected filetype %q, got %q", "c", filetype)
	}

	// Verify that the package: keyword is recognized (case-sensitively)
	rewritten = rewrite(t, "/search?q=searchterm+package%3Ai3-WM")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %q, got %q", "searchterm", querystr)
	}
	if pkg := rewritten.Query().Get("package"); pkg != "i3-WM" {
		t.Fatalf("Expected package %q, got %q", "i3-WM", pkg)
	}

	// Verify that the -package: (negative) keyword is recognized (case-sensitively)
	rewritten = rewrite(t, "/search?q=searchterm+-package%3Ai3-WM")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %q, got %q", "searchterm", querystr)
	}
	if pkg := rewritten.Query().Get("npackage"); pkg != "i3-WM" {
		t.Fatalf("Expected npackage %q, got %q", "i3-WM", pkg)
	}

	// Verify that the pkg: alias is recognized (case-sensitively)
	rewritten = rewrite(t, "/search?q=searchterm+pkg%3Ai3-WM")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %q, got %q", "searchterm", querystr)
	}
	if pkg := rewritten.Query().Get("package"); pkg != "i3-WM" {
		t.Fatalf("Expected package %q, got %q", "i3-WM", pkg)
	}

	// Verify that the -pkg: (negative) alias is recognized (case-sensitively)
	rewritten = rewrite(t, "/search?q=searchterm+-pkg%3Ai3-WM")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %q, got %q", "searchterm", querystr)
	}
	if pkg := rewritten.Query().Get("npackage"); pkg != "i3-WM" {
		t.Fatalf("Expected package %q, got %q", "i3-WM", pkg)
	}

	// Verify that -file is translated to npath
	rewritten = rewrite(t, "/search?q=searchterm+-file:foo")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %q, got %q", "searchterm", querystr)
	}
	file := rewritten.Query().Get("npath")
	if file != "foo" {
		t.Fatalf("Expected npath %q, got %q", "foo", file)
	}

	// Verify that the multiple keywords work as expected
	rewritten = rewrite(t, "/search?q=searchterm+package%3Ai3-WM+filetype%3Ac")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %q, got %q", "searchterm", querystr)
	}
	if pkg := rewritten.Query().Get("package"); pkg != "i3-WM" {
		t.Fatalf("Expected package %q, got %q", "i3-WM", pkg)
	}
	filetype = rewritten.Query().Get("filetype")
	if filetype != "c" {
		t.Fatalf("Expected filetype %q, got %q", "c", filetype)
	}

	// Verify that accessing the map for a keyword that doesn't exist doesn't cause iterations
	rewritten = rewrite(t, "/search?q=searchterm+package%3Ai3-WM")
	vmap := rewritten.Query()["some_array"]
	for _, v := range vmap {
		t.Fatalf("Unexpected value in some_array %q", v)
	}

	// Verify that multiple values for some of the keywords can be passed
	rewritten = rewrite(t, "/search?q=searchterm+-package%3Ai3-WM+-package%3Afoo")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %q, got %q", "searchterm", querystr)
	}
	vmap = rewritten.Query()["npackage"]
	seen := 0
	for _, v := range vmap {
		seen++
		if v != "i3-WM" && v != "foo" {
			t.Fatalf("Unexpected value for -package keyword, got %q", v)
		}
	}
	if seen != 2 {
		t.Fatalf("Expected two elements in the hash of the -package keyword, saw %d", seen)
	}
}
