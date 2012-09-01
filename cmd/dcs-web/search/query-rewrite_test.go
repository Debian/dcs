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

	// Verify that the lang: keyword is properly moved
	rewritten = rewrite(t, "/search?q=searchterm+lang%3Ac")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %s, got %s", "searchterm", querystr)
	}
	lang := rewritten.Query().Get("lang")
	if lang != "c" {
		t.Fatalf("Expected language %s, got %s", "c", lang)
	}

	// Verify that the lang: keyword is treated case-insensitively
	rewritten = rewrite(t, "/search?q=searchterm+lang%3AC")
	querystr = rewritten.Query().Get("q")
	if querystr != "searchterm" {
		t.Fatalf("Expected search query %s, got %s", "searchterm", querystr)
	}
	lang = rewritten.Query().Get("lang")
	if lang != "c" {
		t.Fatalf("Expected language %s, got %s", "c", lang)
	}

}
