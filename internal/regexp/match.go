// Copyright 2011 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package regexp

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp/syntax"
	"sort"

	"github.com/google/codesearch/sparse"
)

// A matcher holds the state for running regular expression search.
type matcher struct {
	prog      *syntax.Prog       // compiled program
	dstate    map[string]*dstate // dstate cache
	start     *dstate            // start state
	startLine *dstate            // start state for beginning of line
	z1, z2    nstate             // two temporary nstates
}

// An nstate corresponds to an NFA state.
type nstate struct {
	q       sparse.Set // queue of program instructions
	partial rune       // partially decoded rune (TODO)
	flag    flags      // flags (TODO)
}

// The flags record state about a position between bytes in the text.
type flags uint32

const (
	flagBOL  flags = 1 << iota // beginning of line
	flagEOL                    // end of line
	flagBOT                    // beginning of text
	flagEOT                    // end of text
	flagWord                   // last byte was word byte
)

// A dstate corresponds to a DFA state.
type dstate struct {
	next     [256]*dstate // next state, per byte
	enc      string       // encoded nstate
	matchNL  bool         // match when next byte is \n
	matchEOT bool         // match in this state at end of text
}

func (z *nstate) String() string {
	return fmt.Sprintf("%v/%#x+%#x", z.q.Dense(), z.flag, z.partial)
}

// enc encodes z as a string.
func (z *nstate) enc() string {
	var buf []byte
	var v [10]byte
	last := ^uint32(0)
	n := binary.PutUvarint(v[:], uint64(z.partial))
	buf = append(buf, v[:n]...)
	n = binary.PutUvarint(v[:], uint64(z.flag))
	buf = append(buf, v[:n]...)
	dense := z.q.Dense()
	ids := make([]int, 0, len(dense))
	for _, id := range z.q.Dense() {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	for _, id := range ids {
		n := binary.PutUvarint(v[:], uint64(uint32(id)-last))
		buf = append(buf, v[:n]...)
		last = uint32(id)
	}
	return string(buf)
}

// dec decodes the encoding s into z.
func (z *nstate) dec(s string) {
	b := []byte(s)
	i, n := binary.Uvarint(b)
	if n <= 0 {
		bug()
	}
	b = b[n:]
	z.partial = rune(i)
	i, n = binary.Uvarint(b)
	if n <= 0 {
		bug()
	}
	b = b[n:]
	z.flag = flags(i)
	z.q.Reset()
	last := ^uint32(0)
	for len(b) > 0 {
		i, n = binary.Uvarint(b)
		if n <= 0 {
			bug()
		}
		b = b[n:]
		last += uint32(i)
		z.q.Add(last)
	}
}

// dmatch is the state we're in when we've seen a match and are just
// waiting for the end of the line.
var dmatch = dstate{
	matchNL:  true,
	matchEOT: true,
}

func init() {
	var z nstate
	dmatch.enc = z.enc()
	for i := range dmatch.next {
		if i != '\n' {
			dmatch.next[i] = &dmatch
		}
	}
}

// init initializes the matcher.
func (m *matcher) init(prog *syntax.Prog) {
	m.prog = prog
	m.dstate = make(map[string]*dstate)

	m.z1.q.Init(uint32(len(prog.Inst)))
	m.z2.q.Init(uint32(len(prog.Inst)))

	m.addq(&m.z1.q, uint32(prog.Start), syntax.EmptyBeginLine|syntax.EmptyBeginText)
	m.z1.flag = flagBOL | flagBOT
	m.start = m.cache(&m.z1)

	m.z1.q.Reset()
	m.addq(&m.z1.q, uint32(prog.Start), syntax.EmptyBeginLine)
	m.z1.flag = flagBOL
	m.startLine = m.cache(&m.z1)

	return
}

// stepEmpty steps runq to nextq expanding according to flag.
func (m *matcher) stepEmpty(runq, nextq *sparse.Set, flag syntax.EmptyOp) {
	nextq.Reset()
	for _, id := range runq.Dense() {
		m.addq(nextq, id, flag)
	}
}

// stepByte steps runq to nextq consuming c and then expanding according to flag.
// It returns true if a match ends immediately before c.
// c is either an input byte or endText.
func (m *matcher) stepByte(runq, nextq *sparse.Set, c int, flag syntax.EmptyOp) (match bool) {
	nextq.Reset()
	m.addq(nextq, uint32(m.prog.Start), flag)
	for _, id := range runq.Dense() {
		i := &m.prog.Inst[id]
		switch i.Op {
		default:
			continue
		case syntax.InstMatch:
			match = true
			continue
		case instByteRange:
			// DCS does not support multi-line matches:
			// match recognizes \n and never calls stepByte(\n).
			// Do not consume \n here to avoid creating dstates
			// that match never needs (for RAM efficiency).
			if c == '\n' {
				break
			}
			if c == endText {
				break
			}
			lo := int((i.Arg >> 8) & 0xFF)
			hi := int(i.Arg & 0xFF)
			ch := c
			if i.Arg&argFold != 0 && 'a' <= ch && ch <= 'z' {
				ch += 'A' - 'a'
			}
			if lo <= ch && ch <= hi {
				m.addq(nextq, i.Out, flag)
			}
		}
	}
	return
}

// addq adds id to the queue, expanding according to flag.
func (m *matcher) addq(q *sparse.Set, id uint32, flag syntax.EmptyOp) {
	if q.Has(id) {
		return
	}
	q.Add(id)
	i := &m.prog.Inst[id]
	switch i.Op {
	case syntax.InstCapture, syntax.InstNop:
		m.addq(q, i.Out, flag)
	case syntax.InstAlt, syntax.InstAltMatch:
		m.addq(q, i.Out, flag)
		m.addq(q, i.Arg, flag)
	case syntax.InstEmptyWidth:
		if syntax.EmptyOp(i.Arg)&^flag == 0 {
			m.addq(q, i.Out, flag)
		}
	}
}

const endText = -1

// computeNext computes the next DFA state if we're in d reading c (an input byte or endText).
func (m *matcher) computeNext(d *dstate, c int) *dstate {
	this, next := &m.z1, &m.z2
	this.dec(d.enc)

	// compute flags in effect before c
	flag := syntax.EmptyOp(0)
	if this.flag&flagBOL != 0 {
		flag |= syntax.EmptyBeginLine
	}
	if this.flag&flagBOT != 0 {
		flag |= syntax.EmptyBeginText
	}
	if this.flag&flagWord != 0 {
		if !isWordByte(c) {
			flag |= syntax.EmptyWordBoundary
		} else {
			flag |= syntax.EmptyNoWordBoundary
		}
	} else {
		if isWordByte(c) {
			flag |= syntax.EmptyWordBoundary
		} else {
			flag |= syntax.EmptyNoWordBoundary
		}
	}
	if c == '\n' {
		flag |= syntax.EmptyEndLine
	}
	if c == endText {
		flag |= syntax.EmptyEndLine | syntax.EmptyEndText
	}

	// re-expand queue using new flags.
	// TODO: only do this when it matters
	// (something is gating on word boundaries).
	m.stepEmpty(&this.q, &next.q, flag)
	this, next = next, this

	// now compute flags after c.
	flag = 0
	next.flag = 0
	if c == '\n' {
		flag |= syntax.EmptyBeginLine
		next.flag |= flagBOL
	}
	if isWordByte(c) {
		next.flag |= flagWord
	}

	// re-add start, process rune + expand according to flags.
	if m.stepByte(&this.q, &next.q, c, flag) {
		return &dmatch
	}
	return m.cache(next)
}

func (m *matcher) cache(z *nstate) *dstate {
	enc := z.enc()
	d := m.dstate[enc]
	if d != nil {
		return d
	}

	// From https://swtch.com/~rsc/regexp/regexp1.html (Russ Cox, January 2007):
	//
	// > One might also worry about bounding the amount of memory used by the
	// > on-the-fly DFA construction. Since the DStates are only a cache of the
	// > step function, the implementation of dstate could choose to throw away
	// > the entire DFA so far if the cache grew too large.
	if len(m.dstate) >= 1000 {
		m.reset()
	}

	d = &dstate{enc: enc}
	m.dstate[enc] = d
	d.matchNL = m.computeNext(d, '\n') == &dmatch
	d.matchEOT = m.computeNext(d, endText) == &dmatch
	return d
}

func (m *matcher) reset() {
	clear(m.start.next[:])
	clear(m.startLine.next[:])
	clear(m.dstate)
	m.dstate[m.start.enc] = m.start
	m.dstate[m.startLine.enc] = m.startLine
}

func (m *matcher) match(b []byte, beginText, endText bool) (end int) {
	//	fmt.Printf("%v\n", m.prog)

	d := m.startLine
	if beginText {
		d = m.start
	}
	//	m.z1.dec(d.enc)
	//	fmt.Printf("%v (%v)\n", &m.z1, d==&dmatch)
	for i, c := range b {
		d1 := d.next[c]
		if d1 == nil {
			if c == '\n' {
				if d.matchNL {
					return i
				}
				d1 = m.startLine
			} else {
				d1 = m.computeNext(d, int(c))
			}
			d.next[c] = d1
		}
		d = d1
		//		m.z1.dec(d.enc)
		//		fmt.Printf("%#U: %v (%v, %v, %v)\n", c, &m.z1, d==&dmatch, d.matchNL, d.matchEOT)
	}
	if d.matchNL || endText && d.matchEOT {
		return len(b)
	}
	return -1
}

func (m *matcher) matchString(b string, beginText, endText bool) (end int) {
	d := m.startLine
	if beginText {
		d = m.start
	}
	for i := 0; i < len(b); i++ {
		c := b[i]
		d1 := d.next[c]
		if d1 == nil {
			if c == '\n' {
				if d.matchNL {
					return i
				}
				d1 = m.startLine
			} else {
				d1 = m.computeNext(d, int(c))
			}
			d.next[c] = d1
		}
		d = d1
	}
	if d.matchNL || endText && d.matchEOT {
		return len(b)
	}
	return -1
}

// isWordByte reports whether the byte c is a word character: ASCII only.
// This is used to implement \b and \B.  This is not right for Unicode, but:
//   - it's hard to get right in a byte-at-a-time matching world
//     (the DFA has only one-byte lookahead)
//   - this crude approximation is the same one PCRE uses
func isWordByte(c int) bool {
	return 'A' <= c && c <= 'Z' ||
		'a' <= c && c <= 'z' ||
		'0' <= c && c <= '9' ||
		c == '_'
}

// TODO:
type Grep struct {
	Regexp          *Regexp   // regexp to search for
	RequiredLiteral []byte    // literal the regexp requires, if non-empty
	Stdout          io.Writer // output target
	Stderr          io.Writer // error target

	L bool // L flag - print file names only
	C bool // C flag - print count of matches
	N bool // N flag - print line numbers
	H bool // H flag - do not print file names

	Match bool

	nr  nlNormReader
	buf []byte // see chunkSize
}

// Grep.Reader processes each file in chunks of this size.
// Most source files are smaller (mean < 10 KiB),
// meaning we fill our buffer with only one read(2) syscall
// and are done with a non-matching file after one bytes.Index call.
//
// The minimum chunkSize is 5 * index.MaxLineLen,
// so that the loop below can find two context lines around each match line.
const chunkSize = 256 * 1024 // 256 KiB

func NewGrep(re *Regexp) *Grep {
	return &Grep{
		Regexp: re,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		buf:    make([]byte, 0, chunkSize),
	}
}

func (g *Grep) AddFlags() {
	flag.BoolVar(&g.L, "l", false, "list matching files only")
	flag.BoolVar(&g.C, "c", false, "print match counts only")
	flag.BoolVar(&g.N, "n", false, "show line numbers")
	flag.BoolVar(&g.H, "h", false, "omit file names")
}

func (g *Grep) File(f *os.File, name string) ([]Match, int64) {
	return g.Reader(f, name), g.nr.read
}

func (g *Grep) OpenFile(name string) ([]Match, int64) {
	f, err := os.Open(name)
	if err != nil {
		fmt.Fprintf(g.Stderr, "%s\n", err)
		return []Match{}, 0
	}
	defer f.Close()
	return g.File(f, name)
}

// nlNormReader is a an io.Reader which normalizes newlines,
// i.e. adds a \n at the end if the underlying reader does not end with one.
type nlNormReader struct {
	underlying io.Reader
	last       byte
	read       int64 // bytes read from underlying
}

func newNlNormReader(r io.Reader) nlNormReader {
	return nlNormReader{
		underlying: r,
		// last starts as '\n' so that an empty file stays empty
		// instead of being turned into a '\n'. Likely irrelevant
		// for DCS (right now), but seems like clean semantics.
		last: '\n',
	}
}

// Read implements io.Reader.
func (nnr *nlNormReader) Read(p []byte) (int, error) {
	n, err := nnr.underlying.Read(p)
	if n > 0 {
		nnr.last = p[n-1]
		nnr.read += int64(n)
	}
	if err == io.EOF && nnr.last != '\n' {
		if n == len(p) {
			// No room left in p. Return a nil error
			// and add the newline in the next Read.
			return n, nil
		}
		// Add a newline to the read buffer.
		p[n] = '\n'
		n++
		nnr.last = '\n'
	}
	return n, err
}

// Match represents a matching line, with its context lines.
//
// All lines are the bytes of the file as they are:
// perhaps invalid UTF-8, not HTML-escaped.
type Match struct {
	Path string
	Line int

	// contents of line (Line - 2)
	Ctxp2 string
	// contents of line (Line - 1)
	Ctxp1 string
	// XXX: The following will most likely change after we figure out how
	// to highlight the found text properly :).
	Context string
	// contents of line (Line + 1)
	Ctxn1 string
	// contents of line (Line + 2)
	Ctxn2 string

	// This will be filled in by the source backend
	PathRank float32
	Ranking  float32
}

// prevLine returns the byte offset of the start of the line
// which comes before the line starting at buf[lineStart:],
// or 0 if there is none. As an example:
//
//	buf contains "first\nsecond\nthird"
//	                     ^       ^ lineStart = 13
//	                     prevLine = 6 ("second")
func prevLine(buf []byte, lineStart int) int {
	return bytes.LastIndexByte(buf[:max(lineStart-1, 0)], '\n') + 1
}

func (g *Grep) Reader(r io.Reader, name string) []Match {
	g.nr = newNlNormReader(r) // store in Grep to avoid per-file allocations
	buf := g.buf[:0]          // reuse g.buf between Reader calls

	searchStart := 0
	lineno := 1
	var result []Match
	for {
		lastChunk := true
		// Fill the existing buffer until it contains up to chunkSize.
		n, err := io.ReadFull(&g.nr, buf[len(buf):cap(buf)])
		switch err {
		case io.ErrUnexpectedEOF:
			// Normal case (only unexpected from io.ReadFull’s POV):
			// buf is not filled because the file’s data is fully read.
		case nil:
			// buf is exactly at chunkSize, another ReadFull is needed.
			lastChunk = false
		case io.EOF:
			// Nothing read: The previous read stopped exactly at end of file.

		default: // err != nil
			fmt.Fprintf(g.Stderr, "%s: %v\n", name, err)
			return result
		}
		buf = buf[:len(buf)+n]

		// If lastChunk is true, we know the last line in buf is not truncated.
		// Otherwise, exclude the last 2 lines from our search,
		// so that we can always unconditionally find 2 context lines
		// when we encounter a match.
		searchEnd := len(buf)
		if !lastChunk {
			lastLineStart := bytes.LastIndexByte(buf, '\n') + 1
			lastCompleteLine := prevLine(buf, lastLineStart)
			searchEnd = prevLine(buf, lastCompleteLine)
		}

		pos := searchStart // always the start of a line
		for pos < searchEnd {
			lineStart := pos
			if req := g.RequiredLiteral; len(req) > 0 {
				reqIdx := bytes.Index(buf[pos:searchEnd], req)
				if reqIdx < 0 {
					break // chunk contains no matches
				}
				// Skip over all lines before the line containing reqIdx.
				lineStart = bytes.LastIndexByte(buf[:pos+reqIdx], '\n') + 1
				lineno += bytes.Count(buf[pos:lineStart], []byte{'\n'})
			}
			lineEnd := lineStart + bytes.IndexByte(buf[lineStart:], '\n')
			pos = lineEnd + 1
			line := buf[lineStart:pos]
			// Unconditionally strip the last character,
			// which is always '\n' thanks to nlNormReader.
			text := line[:len(line)-1]
			beginText := lineno == 1
			const endText = false
			if g.Regexp.Match(line, beginText, endText) >= 0 {
				g.Match = true
				before1 := prevLine(buf, lineStart)
				before2 := prevLine(buf, before1)
				prev2 := bytes.TrimSuffix(buf[before2:before1], []byte{'\n'})
				prev1 := bytes.TrimSuffix(buf[before1:lineStart], []byte{'\n'})
				next1, rest, _ := bytes.Cut(buf[pos:], []byte{'\n'})
				next2, _, _ := bytes.Cut(rest, []byte{'\n'})
				result = append(result, Match{
					Path:    name,
					Line:    lineno,
					Ctxp2:   string(prev2),
					Ctxp1:   string(prev1),
					Context: string(text),
					Ctxn1:   string(next1),
					Ctxn2:   string(next2),
				})
			}
			lineno++
		}
		if lastChunk {
			return result // we know we are done
		}

		// Keep the last two lines in the buffer for the next iteration.
		lineno += bytes.Count(buf[pos:searchEnd], []byte{'\n'})
		before1 := prevLine(buf, searchEnd)
		before2 := prevLine(buf, before1)
		searchStart = searchEnd - before2
		buf = buf[:copy(buf, buf[before2:])]
	}
}
