// Package pforenc contains a Go TurboPFor encoder.
package pforenc

import "math/bits"

// largestP4 is the largest number of bytes our encoder will emit when encoding
// 256 uint32 values in TurboPFor format (safe upper bound for buffers).
const largestP4 = 1025

type blockLayout int

const (
	interleaved blockLayout = iota // full block with interleaved layout (256v)
	sequential                     // remainder block
)

// BlockEncoder encodes one or more TurboPFor blocks, each containing
// at most 256 values (for efficient SIMD processing).
//
// A zero value BlockEncoder is ready to use.
// BlockEncoder is not safe for concurrent use by multiple goroutines.
type BlockEncoder struct{}

// EncodeN encodes len(vals) uint32s into dest, in TurboPFor blocks
// of at most 256 values.
func (be *BlockEncoder) EncodeN(dest []byte, vals []uint32) []byte {
	for len(vals) > 0 {
		chunk := min(len(vals), 256)
		dest = be.EncodeBlock(dest, vals[:chunk])
		vals = vals[chunk:]
	}
	return dest
}

// EncodeBlock encodes len(vals)<=256 uint32s into dest (one TurboPFor block).
func (be *BlockEncoder) EncodeBlock(dest []byte, vals []uint32) []byte {
	if len(vals) < 256 {
		return be.encodeRemainder(dest, vals)
	}
	return be.encodeFull(dest, vals)
}

func (be *BlockEncoder) encodeFull(dest []byte, vals []uint32) []byte {
	return be.encode(dest, vals, interleaved)
}

func (be *BlockEncoder) encodeRemainder(dest []byte, vals []uint32) []byte {
	return be.encode(dest, vals, sequential)
}

func (be *BlockEncoder) encode(dest []byte, vals []uint32, layout blockLayout) []byte {
	bitWidth := maxBitWidth(vals)
	dest = append(dest, byte(bitWidth))
	if bitWidth == 0 {
		return dest
	}
	if layout == interleaved {
		return bitpack256v(dest, vals, bitWidth)
	}
	return bitpack(dest, vals, bitWidth)
}

func maxBitWidth(vals []uint32) int {
	bitWidth := 0
	for _, val := range vals {
		bitWidth = max(bitWidth, bits.Len32(val))
	}
	return bitWidth
}

// StreamEncoder accumulates uint32s until one full TurboPFor block,
// then encodes that block for writing it to disk/network.
//
// A zero value StreamEncoder is ready to use.
// StreamEncoder is not safe for concurrent use by multiple goroutines.
type StreamEncoder struct {
	be   BlockEncoder
	vals [256]uint32     // filled by Add
	n    int             // how many vals
	out  [largestP4]byte // filled by EncodeBlock
}

// Add stores val in the StreamEncoder's buffer.
//
// Contract: When Add returns true (full), you must EncodeBlock().
func (se *StreamEncoder) Add(val uint32) (full bool) {
	se.vals[se.n] = val
	se.n++
	return se.n == len(se.vals)
}

// EncodeBlock encodes the accumulated values in TurboPFor format
// and returns the bytes of the (full or remainder) block.
//
// Contract: the returned bytes are valid until the next EncodeBlock.
func (se *StreamEncoder) EncodeBlock() []byte {
	if se.n == 0 {
		return nil
	}
	dest := se.out[:0]
	dest = se.be.EncodeBlock(dest, se.vals[:se.n])
	se.n = 0
	return dest
}
