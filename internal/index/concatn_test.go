package index

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for filename, content := range files {
		path := filepath.Join(dir, filename)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func createIndex(t *testing.T, sourceDir, indexDir string) {
	t.Helper()

	w, err := Create(indexDir)
	if err != nil {
		t.Fatalf("Create(%s): %v", indexDir, err)
	}

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(sourceDir, entry.Name())
		if err := w.AddFile(path, entry.Name()); err != nil {
			t.Fatalf("AddFile(%s): %v", entry.Name(), err)
		}
	}

	if err := w.Flush(); err != nil {
		t.Fatalf("Flush(): %v", err)
	}
}

type searchResult struct {
	Docid    uint32
	Filename string
	Position uint32 // byte position within the file (for positional queries)
}

func result(docid uint32, filename string, pos uint32) searchResult {
	return searchResult{
		Docid:    docid,
		Filename: filename,
		Position: pos,
	}
}

func searchIndex(t *testing.T, indexDir string, query string) []searchResult {
	t.Helper()

	if len(query) < 4 {
		t.Fatalf("Query must be at least 4 characters for positional search, got %q", query)
	}

	idx, err := Open(indexDir)
	if err != nil {
		t.Fatalf("Failed to open index: %v", err)
	}
	defer idx.Close()

	matches, err := idx.QueryPositional(query)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil // no matches
		}
		t.Fatalf("QueryPositional(%q): %v", query, err)
	}

	results := make([]searchResult, 0, len(matches))
	for _, match := range matches {
		filename, err := idx.DocidMap.Lookup(match.Docid)
		if err != nil {
			t.Fatalf("Failed to lookup docid %d: %v", match.Docid, err)
		}
		results = append(results, searchResult{
			Docid:    match.Docid,
			Filename: filename,
			Position: match.Position,
		})
	}

	return results
}

func TestConcatN(t *testing.T) {
	tmpDir := t.TempDir()

	// Write dummy source code files and create one index per directory.
	src1Dir := filepath.Join(tmpDir, "src1")
	writeFiles(t, src1Dir, map[string]string{
		"file1.txt": "abc def ghi",
		"file2.txt": "abc jkl mno",
	})
	createIndex(t, src1Dir, src1Dir+".idx")

	src2Dir := filepath.Join(tmpDir, "src2")
	writeFiles(t, src2Dir, map[string]string{
		"file3.txt": "pqr stu abc def",
		"file4.txt": "vwx yz ghi",
	})
	createIndex(t, src2Dir, src2Dir+".idx")

	src3Dir := filepath.Join(tmpDir, "src3")
	writeFiles(t, src3Dir, map[string]string{
		"file5.txt": "abc def ghi jkl",
	})
	createIndex(t, src3Dir, src3Dir+".idx")

	// Merge the individual indexes into one large index.
	dest := filepath.Join(tmpDir, "merged")
	err := ConcatN(dest, []string{
		src1Dir + ".idx",
		src2Dir + ".idx",
		src3Dir + ".idx",
	})
	if err != nil {
		t.Fatalf("ConcatN failed: %v", err)
	}

	for _, tt := range []struct {
		query string
		want  []searchResult
	}{
		{
			query: "abc ",
			want: []searchResult{
				result(0, "file1.txt", 0),
				result(1, "file2.txt", 0),
				result(2, "file3.txt", 8),
				result(4, "file5.txt", 0),
			},
		},

		{
			query: " def",
			want: []searchResult{
				result(0, "file1.txt", 3),
				result(2, "file3.txt", 11),
				result(4, "file5.txt", 3),
			},
		},
	} {
		results := searchIndex(t, dest, tt.query)
		if diff := cmp.Diff(tt.want, results); diff != "" {
			t.Errorf("searchIndex(%q): unexpected results: diff (-want +got):\n%s", tt.query, diff)
		}
	}
}

func TestConcatNEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	// Create empty source directory and index it
	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	createIndex(t, srcDir, srcDir+".idx")

	dest := filepath.Join(tmpDir, "merged")
	if err := ConcatN(dest, []string{srcDir + ".idx"}); err != nil {
		t.Fatalf("ConcatN failed on empty index: %v", err)
	}

	if results := searchIndex(t, dest, "test"); len(results) != 0 {
		t.Errorf("Expected 0 results from empty index, got %d", len(results))
	}
}

func TestConcatNSingleSource(t *testing.T) {
	tmpDir := t.TempDir()

	srcDir := filepath.Join(tmpDir, "src")
	writeFiles(t, srcDir, map[string]string{
		"test.txt": "hello world test",
	})
	createIndex(t, srcDir, srcDir+".idx")

	dest := filepath.Join(tmpDir, "merged")
	if err := ConcatN(dest, []string{srcDir + ".idx"}); err != nil {
		t.Fatalf("ConcatN failed with single source: %v", err)
	}

	for _, tt := range []struct {
		query string
		want  []searchResult
	}{
		{
			query: "hell",
			want: []searchResult{
				result(0, "test.txt", 0),
			},
		},
	} {
		results := searchIndex(t, dest, tt.query)
		if diff := cmp.Diff(tt.want, results); diff != "" {
			t.Errorf("searchIndex(%q): unexpected results: diff (-want +got):\n%s", tt.query, diff)
		}
	}
}

func TestConcatNMany(t *testing.T) {
	tmpDir := t.TempDir()

	numSources := 5
	filesPerSource := 100

	var indexes []string
	for i := range numSources {
		srcDir := filepath.Join(tmpDir, fmt.Sprintf("src%d", i))

		files := make(map[string]string)
		for j := range filesPerSource {
			filename := fmt.Sprintf("file_%d_%d.txt", i, j)
			content := fmt.Sprintf("file %d %d contains abc def ghi", i, j)
			files[filename] = content
		}
		writeFiles(t, srcDir, files)

		// Create index for this source
		idxDir := filepath.Join(tmpDir, fmt.Sprintf("idx%d", i))
		createIndex(t, srcDir, idxDir)
		indexes = append(indexes, idxDir)
	}

	dest := filepath.Join(tmpDir, "merged")
	if err := ConcatN(dest, indexes); err != nil {
		t.Fatalf("ConcatN failed: %v", err)
	}

	const query = "cont"
	results := searchIndex(t, dest, query)
	wantTotal := numSources * filesPerSource
	if len(results) != wantTotal {
		t.Errorf("Expected %d files containing %q, got %d", wantTotal, query, len(results))
	}

	for _, result := range results {
		if !strings.HasPrefix(result.Filename, "file_") ||
			!strings.HasSuffix(result.Filename, ".txt") {
			t.Errorf("Unexpected file name format: %q", result.Filename)
		}
	}
}
