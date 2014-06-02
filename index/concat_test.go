// Copyright 2011 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package index

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestConcat(t *testing.T) {
	var mergePaths1 = []string{
		"/a",
		"/b",
		"/c",
	}

	var mergePaths2 = []string{
		"/b",
		"/cc",
	}

	var mergeFiles1 = map[string]string{
		"/a/x":  "hello world",
		"/a/y":  "goodbye world",
		"/b/xx": "now is the time",
		"/b/xy": "for all good men",
		"/c/ab": "give me all the potatoes",
		"/c/de": "or give me death now",
	}

	var mergeFiles2 = map[string]string{
		"/cc":    "come to the aid of his potatoes",
		"/d/www": "world wide indeed",
		"/d/xz":  "no, not now",
		"/d/yy":  "first potatoes, now liberty?",
	}

	f1, _ := ioutil.TempFile("", "index-test")
	f2, _ := ioutil.TempFile("", "index-test")
	f3, _ := ioutil.TempFile("", "index-test")
	defer os.Remove(f1.Name())
	defer os.Remove(f2.Name())
	defer os.Remove(f3.Name())

	out1 := f1.Name()
	out2 := f2.Name()
	out3 := f3.Name()

	buildIndex(out1, mergePaths1, mergeFiles1)
	buildIndex(out2, mergePaths2, mergeFiles2)

	Concat(out3, out1, out2)

	ix1 := Open(out1)
	ix2 := Open(out2)
	ix3 := Open(out3)

	nameof := func(ix *Index) string {
		switch {
		case ix == ix1:
			return "ix1"
		case ix == ix2:
			return "ix2"
		case ix == ix3:
			return "ix3"
		}
		return "???"
	}

	checkFiles := func(ix *Index, l ...string) {
		for i, s := range l {
			if n := ix.Name(uint32(i)); n != s {
				t.Errorf("%s: Name(%d) = %s, want %s", nameof(ix), i, n, s)
			}
		}
	}

	checkFiles(ix1, "/a/x", "/a/y", "/b/xx", "/b/xy", "/c/ab", "/c/de")
	checkFiles(ix2, "/cc", "/d/www", "/d/xz", "/d/yy")

	checkFiles(ix3, "/a/x", "/a/y", "/b/xx", "/b/xy", "/c/ab", "/c/de", "/cc", "/d/www", "/d/xz", "/d/yy")

	check := func(ix *Index, trig string, l ...uint32) {
		l1 := ix.PostingList(tri(trig[0], trig[1], trig[2]))
		if !equalList(l1, l) {
			t.Errorf("PostingList(%s, %s) = %v, want %v", nameof(ix), trig, l1, l)
		}
	}

	check(ix1, "wor", 0, 1)
	check(ix1, "now", 2, 5)
	check(ix1, "all", 3, 4)

	check(ix2, "now", 2, 3)

	check(ix3, "all", 3, 4)
	check(ix3, "wor", 0, 1, 7)
	check(ix3, "now", 2, 5, 8, 9)
	check(ix3, "pot", 4, 6, 9)
}
