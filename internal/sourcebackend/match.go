package sourcebackend

import (
	"bytes"
	"fmt"
	"os"

	"github.com/Debian/dcs/internal/index"
	"github.com/Debian/dcs/internal/regexp"
)

// matcher is a wrapper around internal/regexp.Grep
// which verifies positional index matches where beneficial
// (saves candidate bytes and hence query time).
type matcher struct {
	grep *regexp.Grep

	buf      []byte // for (*os.File).ReadAt
	literalb []byte // for bytes.Equal
}

func newMatcher(g *regexp.Grep, queryPos bool, literal string) *matcher {
	if !queryPos {
		return &matcher{
			grep: g,
			// literalb is nil, i.e. no positional index verification.
		}
	}
	return &matcher{
		grep:     g,
		buf:      make([]byte, len(literal)),
		literalb: []byte(literal),
	}
}

func (m *matcher) Match(name string, matches []index.Match) ([]regexp.Match, int64) {
	f, err := os.Open(name)
	if err != nil {
		// TODO: refactor error handling
		fmt.Fprintf(m.grep.Stderr, "%s\n", err)
		return nil, 0
	}
	defer f.Close()

	if m.literalb == nil {
		// Not a literal query. Grep immediately.
		return m.grep.File(f, name)
	}

	falsePositive, verified := m.maybeVerifyPositions(f, matches)
	if falsePositive {
		// This index match was a false positive:
		// all matches were checked, none were found.
		// We can safely skip grep'ing this file.
		return nil, verified
	}
	ms, grepped := m.grep.File(f, name)
	return ms, grepped + verified
}

func (m *matcher) maybeVerifyPositions(f *os.File, matches []index.Match) (falsePositive bool, read int64) {
	fi, err := f.Stat()
	if err != nil {
		return false, 0
	}

	fullReadBytes := fi.Size()
	// For each possible match, the kernel might have to read a full page.
	// (Assume the worst case, i.e. possible matches 4K+ apart.)
	verifyBytes := int64(len(matches)) * 4096
	if verifyBytes >= fullReadBytes {
		return false, 0 // likely cheaper to just grep the file
	}

	// We have few enough matches in a large-enough file
	// that this extra verification step makes sense.
	for _, c := range matches {
		n, _ := f.ReadAt(m.buf, int64(c.Position))
		read += int64(n)
		if n == len(m.buf) && bytes.Equal(m.buf, m.literalb) {
			// Not a false positive: the file contains real matches.
			return false, read
		}
	}
	// All candidates verified, none a real match:
	// This file is a false positive.
	return true, read
}
