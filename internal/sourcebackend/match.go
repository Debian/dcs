package sourcebackend

import (
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
			// literal is nil, i.e. no positional index verification.
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

	if m.literalb != nil {
		// TODO: verify positional index matches using ReadAt
	}

	return m.grep.File(f, name)
}
