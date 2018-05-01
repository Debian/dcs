package index

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Debian/dcs/index2"
	"github.com/google/codesearch/sparse"
)

type countingWriter struct {
	offset uint64
	f      *os.File
	bufw   *bufio.Writer
}

func newCountingWriter(f *os.File) countingWriter {
	return countingWriter{
		f:    f,
		bufw: bufio.NewWriter(f),
	}
}

func (cw *countingWriter) Close() error {
	if err := cw.bufw.Flush(); err != nil {
		return err
	}
	if err := cw.f.Close(); err != nil {
		return err
	}
	return nil
}

func (cw *countingWriter) Write(p []byte) (n int, err error) {
	n, err = cw.bufw.Write(p)
	cw.offset += uint64(n)
	return n, err
}

type entry struct {
	docid    uint32
	position uint32
}

type Writer struct {
	dir   string
	index map[Trigram][]entry
	docs  []string
	set   *sparse.Set // efficiently reset across AddFile calls
	inbuf []byte
}

func Create(dir string) (*Writer, error) {
	const maxTrigram = 1 << 24
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &Writer{
		dir:   dir,
		index: make(map[Trigram][]entry),
		set:   sparse.NewSet(maxTrigram),
		inbuf: make([]byte, 16384),
	}, nil
}

// An IgnoreFunc returns a non-nil error describing the reason why the file
// should not be added to the index, or nil if it should be added.
type IgnoreFunc func(info os.FileInfo, dir string, fn string) error

// An ErrFunc is called for each error adding a file to the index, allowing the
// caller to take action (e.g. delete the not added files to conserve disk
// space). If the ErrFunc returns a non-nil error, indexing is aborted.
type ErrFunc func(path string, info os.FileInfo, err error) error

// A SuccessFunc is called for each file which was successfully added to the
// index.
type SuccessFunc func(path string, info os.FileInfo) error

func (w *Writer) AddDir(dir string, trimPrefix string, ignored IgnoreFunc, ef ErrFunc, sf SuccessFunc) error {
	if ef == nil {
		ef = func(_ string, _ os.FileInfo, _ error) error { return nil }
	}
	if sf == nil {
		sf = func(_ string, _ os.FileInfo) error { return nil }
	}
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if info == nil {
			return nil
		}

		if dir, filename := filepath.Split(path); filename != "" {
			if skip := ignored(info, dir, filename); skip != nil {
				if err := ef(path, info, skip); err != nil {
					return err
				}
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		if err := w.AddFile(path, strings.TrimPrefix(path, trimPrefix)); err != nil {
			return ef(path, info, err)
		}
		return sf(path, info)
	})
}

// validUTF8 reports whether the byte pair can appear in a
// valid sequence of UTF-8-encoded code points.
func validUTF8(c1, c2 uint32) bool {
	switch {
	case c1 < 0x80:
		// 1-byte, must be followed by 1-byte or first of multi-byte
		return c2 < 0x80 || 0xc0 <= c2 && c2 < 0xf8
	case c1 < 0xc0:
		// continuation byte, can be followed by nearly anything
		return c2 < 0xf8
	case c1 < 0xf8:
		// first of multi-byte, must be followed by continuation byte
		return 0x80 <= c2 && c2 < 0xc0
	}
	return false
}

// Tuning constants for detecting text files.
// A file is assumed not to be text files (and thus not indexed)
// if it contains an invalid UTF-8 sequences, if it is longer than maxFileLength
// bytes, if it contains a line longer than maxLineLen bytes,
// or if it contains more than maxTextTrigrams distinct trigrams.
const (
	maxFileLen      = 1 << 30
	maxLineLen      = 2000
	maxTextTrigrams = 20000
)

func (w *Writer) AddFile(fn, name string) error {
	w.set.Reset()
	docid := uint32(len(w.docs))
	w.docs = append(w.docs, name)
	f, err := os.Open(fn)
	if err != nil {
		return err
	}
	defer f.Close()

	var (
		c       byte
		tv      uint32
		i       = 0
		n       = 0
		linelen = 0
		buf     = w.inbuf[:0]
		entries = make(map[Trigram][]entry)
	)
	for {
		tv = (tv << 8) & (1<<24 - 1)
		if i >= len(buf) {
			n, err := f.Read(buf[:cap(buf)])
			if n == 0 {
				if err != nil {
					if err == io.EOF {
						break
					}
					return err
				}
				return errors.New("0-length read")
			}
			buf = buf[:n]
			i = 0
		}
		c = buf[i]
		i++
		tv |= uint32(c)
		if linelen++; linelen > maxLineLen {
			return errors.New("very long lines, ignoring")
		}
		if c == '\n' {
			linelen = 0
		}
		if !validUTF8((tv>>8)&0xFF, tv&0xFF) {
			return errors.New("invalid UTF-8, ignoring")
		}
		if n++; n >= 3 {
			w.set.Add(tv)
			t := Trigram(tv)
			entries[t] = append(entries[t], entry{docid: docid, position: uint32(n - 3)})
		}
		if n > maxFileLen {
			return errors.New("too long, ignoring")
		}
	}
	if w.set.Len() > maxTextTrigrams {
		return errors.New("too many trigrams, probably not text, ignoring")
	}
	for t, entries := range entries {
		w.index[t] = append(w.index[t], entries...)
	}
	return nil
}

func (w *Writer) Flush() error {
	if err := w.writeDocidMap(w.docs); err != nil {
		return err
	}

	// Sort the trigrams by value to create a deterministic index:
	trigrams := make([]Trigram, 0, len(w.index))
	for t := range w.index {
		trigrams = append(trigrams, t)
	}
	sort.Slice(trigrams, func(i, j int) bool { return trigrams[i] < trigrams[j] })

	if err := w.writeDocid(trigrams); err != nil {
		return err
	}

	if err := w.writePos(trigrams); err != nil {
		return err
	}

	if err := w.writePosrel(trigrams); err != nil {
		return err
	}

	return nil
}

// writeDocidMap creates the index’s docid.map file, which is a list of
// \n-separated strings (for easy printing by humans using less(1) or
// strings(1)), followed by the byte offsets of each entry and, lastly, the
// offset of the byte offsets (for fast lookup).
func (w *Writer) writeDocidMap(filenames []string) error {
	f, err := os.Create(filepath.Join(w.dir, "docid.map"))
	if err != nil {
		return err
	}
	defer f.Close()
	cw := newCountingWriter(f)
	offsets := make([]uint32, len(filenames))
	for idx, fn := range filenames {
		offsets[idx] = uint32(cw.offset)
		fmt.Fprintln(&cw, fn)
	}
	indexStart := uint32(cw.offset)
	if err := binary.Write(&cw, binary.LittleEndian, offsets); err != nil {
		return err
	}
	if err := binary.Write(&cw, binary.LittleEndian, indexStart); err != nil {
		return err
	}
	return cw.Close()
}

func (w *Writer) writeDocid(trigrams []Trigram) error {
	f, err := os.Create(filepath.Join(w.dir, "posting.docid.meta"))
	if err != nil {
		return err
	}
	defer f.Close()
	bufw := bufio.NewWriter(f)
	dw, err := index2.NewPForWriter(w.dir, "docid")
	if err != nil {
		return err
	}
	defer dw.Close()
	for _, t := range trigrams {
		//log.Printf("trigram %c%c%c (docid)", (t>>16)&0xFF, (t>>8)&0xFF, (t>>0)&0xFF)
		entries := w.index[t]
		me := MetaEntry{
			Trigram:    t,
			Entries:    1,
			OffsetData: dw.Offset(),
		}

		// The first entry will always be stored, even if 0
		prev := entries[0].docid
		dw.PutUint32(prev)
		for _, entry := range entries[1:] {
			delta := entry.docid - prev
			if delta == 0 {
				continue
			}
			dw.PutUint32(delta)
			prev = entry.docid
			me.Entries++
		}

		if err := dw.Flush(); err != nil {
			return err
		}

		if err := binary.Write(bufw, binary.LittleEndian, &me); err != nil {
			return err
		}
	}
	if err := dw.Close(); err != nil {
		return err
	}
	if err := bufw.Flush(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}

func (w *Writer) writePos(trigrams []Trigram) error {
	f, err := os.Create(filepath.Join(w.dir, "posting.pos.meta"))
	if err != nil {
		return err
	}
	defer f.Close()
	bufw := bufio.NewWriter(f)
	dw, err := index2.NewPForWriter(w.dir, "pos")
	if err != nil {
		return err
	}
	defer dw.Close()
	for _, t := range trigrams {
		entries := w.index[t]

		if err := binary.Write(bufw, binary.LittleEndian, &MetaEntry{
			Trigram:    t,
			Entries:    uint32(len(entries)), // TODO: assert
			OffsetData: dw.Offset(),
		}); err != nil {
			return err
		}

		var prevDocid, prevPos uint32
		for _, entry := range entries {
			if prevDocid != entry.docid {
				prevPos = 0
				prevDocid = entry.docid
			}
			dw.PutUint32(entry.position - prevPos)
			prevPos = entry.position
		}

		if err := dw.Flush(); err != nil {
			return err
		}
	}
	if err := dw.Close(); err != nil {
		return err
	}
	if err := bufw.Flush(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	return nil
}

func (w *Writer) writePosrel(trigrams []Trigram) error {
	f, err := os.Create(filepath.Join(w.dir, "posting.posrel.meta"))
	if err != nil {
		return err
	}
	defer f.Close()
	bufw := bufio.NewWriter(f)
	df, err := os.Create(filepath.Join(w.dir, "posting.posrel.data"))
	if err != nil {
		return err
	}
	defer df.Close()
	dcw := newCountingWriter(df)
	pw := index2.NewPosrelWriter(&dcw)
	for _, t := range trigrams {
		entries := w.index[t]

		if err := binary.Write(bufw, binary.LittleEndian, &MetaEntry{
			Trigram:    t,
			OffsetData: int64(dcw.offset),
		}); err != nil {
			return err
		}

		prevDocid := uint32(math.MaxUint32)
		for _, entry := range entries {
			var chg byte
			if prevDocid != entry.docid {
				chg = 1
				prevDocid = entry.docid
			}
			if err := pw.Write([]byte{chg}, 1); err != nil {
				return err
			}
		}
		if err := pw.Flush(); err != nil {
			return err
		}
	}
	if err := bufw.Flush(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := dcw.Close(); err != nil {
		return err
	}

	return nil
}
