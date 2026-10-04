package web

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"sort"
)

// disksort.go implements sorting search result pointers such that
// most queries will be handled entirely in-memory (RAM),
// but large queries are handled by spilling result pointers
// to disk and then using merge sort to produce one sorted stream.

type sortedPointers interface {
	// Len returns the number of resultPointers.
	Len() int

	// Slice returns the resultPointers from start to end, in ranking order.
	//
	// Used by writeResults()
	Slice(start, end int) ([]resultPointer, error)

	// All returns an iterator over all resultPointers.
	//
	// Used by writeToDisk().
	All() iter.Seq2[resultPointer, error]
}

type memPointers []resultPointer

// Len implements sortedPointers.
func (mp memPointers) Len() int { return len(mp) }

// Slice implements sortedPointers.
func (mp memPointers) Slice(start, end int) ([]resultPointer, error) {
	return mp[start:end], nil
}

// All implements sortedPointers.
func (mp memPointers) All() iter.Seq2[resultPointer, error] {
	return func(yield func(resultPointer, error) bool) {
		for _, p := range mp {
			if !yield(p, nil) {
				return
			}
		}
	}
}

type diskWriter struct {
	// config
	flushThreshold int
	dir            string

	// state
	buf  []resultPointer
	runs []string
}

func (dw *diskWriter) Add(p resultPointer) error {
	if len(dw.buf) == dw.flushThreshold {
		if err := dw.flushRunToDisk(); err != nil {
			return err
		}
		// dw.buf is now empty
	}
	dw.buf = append(dw.buf, p)
	return nil
}

func (dw *diskWriter) flushRunToDisk() error {
	fn := filepath.Join(dw.dir, fmt.Sprintf("pointers.run.%03d.bin", len(dw.runs)))
	f, err := os.Create(fn)
	if err != nil {
		return err
	}
	defer f.Close()
	sort.Sort(pointerByRanking(dw.buf))
	// Use bufio to ammortize syscalls: one write(2) per 4096 bytes.
	bufw := bufio.NewWriter(f)
	var ptrBuf [32]byte
	for _, ptr := range dw.buf {
		ptr.Marshal(&ptrBuf)
		bufw.Write(ptrBuf[:])
	}
	if err := bufw.Flush(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	dw.runs = append(dw.runs, fn)
	dw.buf = dw.buf[:0]
	return nil
}

func (dw *diskWriter) Flush() (sortedPointers, error) {
	if len(dw.runs) == 0 {
		// Easy case: all results fit into memory.
		sort.Sort(pointerByRanking(dw.buf))
		return memPointers(dw.buf), nil
	}

	n := len(dw.runs)*dw.flushThreshold + len(dw.buf)

	// Too many results to fit into memory. Merge sort all runs.
	if len(dw.buf) > 0 {
		if err := dw.flushRunToDisk(); err != nil {
			return nil, err
		}
	}

	dp := diskPointers{
		runSize: dw.flushThreshold,
		order:   filepath.Join(dw.dir, "pointers.order"),
		n:       n,
		runs:    dw.runs,
	}
	if err := dp.traverseRecordOrder(); err != nil {
		return nil, err
	}
	return dp, nil
}

type diskPointers struct {
	runSize int
	order   string
	n       int
	runs    []string
}

// traverseRecordOrder traverses all runs in ranking order
// and records this order into the order file, so that
// random access (requesting page N) into the results is fast.
func (dp diskPointers) traverseRecordOrder() error {
	order, err := os.Create(dp.order)
	if err != nil {
		return err
	}
	defer order.Close()
	bufw := bufio.NewWriter(order)
	var b [4]byte
	err = dp.merge(func(_ resultPointer, index uint32) bool {
		binary.LittleEndian.PutUint32(b[:], index)
		bufw.Write(b[:])
		return true
	})
	if err != nil {
		return err
	}
	if err := bufw.Flush(); err != nil {
		return err
	}
	return order.Close()
}

// Len implements sortedPointers.
func (dp diskPointers) Len() int { return dp.n }

// Slice implements sortedPointers.
func (dp diskPointers) Slice(start, end int) ([]resultPointer, error) {
	// Read the requested (uint32) positions.
	f, err := os.Open(dp.order)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	order := make([]byte, (end-start)*4)
	if _, err := f.ReadAt(order, int64(start)*4); err != nil {
		return nil, err
	}

	// Then read the resultpointers these positions refer to.
	// Files are opened on demand, so we usually need 1 or 2 opens.
	runFiles := make([]*os.File, len(dp.runs))
	defer func() {
		for _, f := range runFiles {
			if f != nil {
				f.Close()
			}
		}
	}()

	pointers := make([]resultPointer, end-start)
	var b [32]byte
	for i := range pointers {
		idx := int(binary.LittleEndian.Uint32(order[i*4:]))
		run := idx / dp.runSize
		if runFiles[run] == nil {
			runFiles[run], err = os.Open(dp.runs[run])
			if err != nil {
				return nil, err
			}
		}
		// Calculate the offset within this runFile.
		offset := int64(idx % dp.runSize * len(b))
		if _, err := runFiles[run].ReadAt(b[:], offset); err != nil {
			return nil, err
		}
		pointers[i].Unmarshal(&b)
	}
	return pointers, nil
}

// All implements sortedPointers.
func (dp diskPointers) All() iter.Seq2[resultPointer, error] {
	return func(yield func(resultPointer, error) bool) {
		err := dp.merge(func(p resultPointer, _ uint32) bool {
			return yield(p, nil)
		})
		if err != nil {
			yield(resultPointer{}, err)
		}
	}
}

type runFile struct {
	r   *bufio.Reader
	buf [32]byte
	idx int
}

func (r *runFile) read(p *resultPointer) error {
	if _, err := io.ReadFull(r.r, r.buf[:]); err != nil {
		return err
	}
	p.Unmarshal(&r.buf)
	r.idx++
	return nil
}

// merge traverses all dp.runs files in ranking order.
func (dp diskPointers) merge(fn func(p resultPointer, index uint32) bool) error {
	runFiles := make([]*runFile, 0, len(dp.runs))
	next := make(pointerByRanking, 0, len(dp.runs))
	for idx, fn := range dp.runs {
		f, err := os.Open(fn)
		if err != nil {
			return err
		}
		defer f.Close()
		r := &runFile{
			r:   bufio.NewReader(f),
			idx: idx*dp.runSize - 1, // will be incremented by runFile.read
		}
		var p resultPointer
		if err := r.read(&p); err != nil {
			return err
		}
		runFiles = append(runFiles, r)
		next = append(next, p)
	}
	for len(runFiles) > 0 {
		// Find the highest-ranked result pointer of all open runFiles.
		best := 0
		for i := range next {
			if next.Less(i, best) {
				best = i
			}
		}
		if !fn(next[best], uint32(runFiles[best].idx)) {
			return nil
		}
		err := runFiles[best].read(&next[best])
		if err == io.EOF {
			// All pointers of this runFile have been read.
			runFiles = slices.Delete(runFiles, best, best+1)
			next = slices.Delete(next, best, best+1)
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}
