// Copyright 2011 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package index

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestConcatN(t *testing.T) {
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

	var mergeFiles3 = map[string]string{
		"/latest": "ZZZ makes the snoring rabbit",
		"/latest2": "aaa makes the snoring armadillo",
	}

	f1, _ := ioutil.TempFile("", "index-test")
	f2, _ := ioutil.TempFile("", "index-test")
	f3, _ := ioutil.TempFile("", "index-test")
	f4, _ := ioutil.TempFile("", "index-test")
	defer os.Remove(f1.Name())
	defer os.Remove(f2.Name())
	defer os.Remove(f4.Name())

	out1 := f1.Name()
	out2 := f2.Name()
	out3 := f3.Name()
	out4 := f4.Name()

	buildIndex(out1, mergePaths1, mergeFiles1)
	buildIndex(out2, mergePaths2, mergeFiles2)
	buildIndex(out3, []string{}, mergeFiles3)

	ConcatN(out4, out1, out2, out3)

	ix1 := Open(out1)
	ix2 := Open(out2)
	ix3 := Open(out3)
	ix4 := Open(out4)

	nameof := func(ix *Index) string {
		switch {
		case ix == ix1:
			return "ix1"
		case ix == ix2:
			return "ix2"
		case ix == ix3:
			return "ix3"
		case ix == ix4:
			return "ix4"
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
	checkFiles(ix3, "/latest")

	checkFiles(ix4, "/a/x", "/a/y", "/b/xx", "/b/xy", "/c/ab", "/c/de", "/cc", "/d/www", "/d/xz", "/d/yy", "/latest")

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

	check(ix3, "ZZZ", 0)
	check(ix3, "aaa", 1)

	check(ix4, "all", 3, 4)
	check(ix4, "wor", 0, 1, 7)
	check(ix4, "now", 2, 5, 8, 9)
	check(ix4, "pot", 4, 6, 9)
	check(ix4, "ZZZ", 10)
	check(ix4, "aaa", 11)
}
