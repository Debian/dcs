package web

import "iter"

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
