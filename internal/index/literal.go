package index

import "regexp/syntax"

// LongestLiteral extracts the longest literal string
// which every match of regular expression re must contain.
//
// For example, for the regular expression “hash.*rational”,
// every match must contain both “hash” and “rational”.
func LongestLiteral(re *syntax.Regexp) string {
	var w longestLitWalker
	return w.walk(re)
}

type longestLitWalker struct {
	sequence []rune // characters (runes) seen, so far
	longest  string // longest sequence that ended, so far
}

func (w *longestLitWalker) walk(re *syntax.Regexp) string {
	switch re.Op {
	case syntax.OpLiteral:
		if re.Flags&syntax.FoldCase != 0 {
			// For case-insensitive literals ((?i) option),
			// there is no single sequence that must match,
			// there are too many options. End the sequence.
			w.endSequence()
			return w.longest
		}
		w.sequence = append(w.sequence, re.Rune...)

	case syntax.OpConcat:
		for _, sub := range re.Sub {
			w.walk(sub)
		}

		// TODO: cover more re.Op types so that more queries
		// can benefit from this optimization

	default:
		// If in doubt, the sequence must be ended.
		w.endSequence()
	}
	w.endSequence()
	return w.longest
}

func (w *longestLitWalker) endSequence() {
	if s := string(w.sequence); len(s) > len(w.longest) {
		w.longest = s
	}
	w.sequence = w.sequence[:0] // reuse the memory
}
