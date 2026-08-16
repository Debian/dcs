// Package pforenc contains a Go TurboPFor encoder.
package pforenc

import (
	"encoding/binary"
	"math/bits"
)

// largestP4 is the largest number of bytes our encoder will emit when encoding
// 256 uint32 values in TurboPFor format (safe upper bound for buffers).
const largestP4 = 1025

type blockLayout int

const (
	interleaved blockLayout = iota // full block with interleaved layout (256v)
	sequential                     // remainder block
)

type blockType int

const (
	blockBitpacking blockType = iota
	blockBitpackingVBExceptions
	blockBitpackingExceptions
	blockConstant
)

// BlockEncoder encodes one or more TurboPFor blocks, each containing
// at most 256 values (for efficient SIMD processing).
//
// A zero value BlockEncoder is ready to use.
// BlockEncoder is not safe for concurrent use by multiple goroutines.
type BlockEncoder struct {
	buf   [4]byte
	exmap [32]byte    // exception bitmap
	exidx [256]byte   // exception index (vb)
	high  [256]uint32 // exception high bits
}

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
	var stats stats
	scan(&stats, vals)
	bitWidth := bits.Len32(stats.or)
	if stats.or == stats.and {
		return be.encodeConstant(dest, vals, bitWidth)
	}
	n := len(vals)
	bestType := blockBitpacking
	bestB := bitWidth
	best := priceBitpack(n, bitWidth, layout)

	// Walk from high bitWidths to low: to break ties, we prefer
	// the encoding with fewer exceptions (for faster decoding).
	for b := bitWidth - 1; b >= 0; b-- {
		nex := int(stats.cnt[b])
		size := priceBitpackExceptions(n, b, bitWidth, nex, layout)
		if size < best {
			bestType = blockBitpackingExceptions
			bestB = b
			best = size
		}
		// Over-approximate the number of VB bytes.
		vb := nex + // exceptions using 1, 2, 3, 4, or 5 VB bytes
			int(stats.cnt[b+7]+ // exceptions using 2, 3, 4, or 5 VB bytes
				stats.cnt[b+14]+ // exceptions using 3, 4, or 5 VB bytes
				stats.cnt[b+19]+ // exceptions using 4 or 5 VB bytes
				stats.cnt[b+24]) // exceptions using 5 VB bytes
		size = headerBytes + headerExBytes + payloadBytes(n, b, layout) + vb + nex
		if size < best {
			// NOTE: nex=256 (unrepresentable in the TurboPFor header)
			// can never end up here in the VB exceptions branch:
			//
			// If nex=256, we started (above the loop) with:
			//
			//   best := priceBitpack() = 1 + 32*bitWidth
			//
			// In this loop iteration, we are evaluating VB(b):
			//
			//   VB(b) >= 2 + 32*b + 256 /* index */ + 256 /* VB */
			//   i.e. VB(b) >= 2 + 32*b + 512
			//
			// VB(b) has a smaller size than bitpacking, so:
			//
			//   2 + 32*b + 512 <= VB(b) < 1 + 32*bitWidth = bitpack(bitWidth)
			//   i.e. 513 < 32*(bitWidth-b)
			//   i.e. bitWidth-b >= 17
			//
			// So when evaluating VB(b), the earlier VB(b+14) was also checked,
			// and it is always strictly cheaper:
			//
			//   VB(b+14) uses 32*14 extra payload bytes,
			//   but each exception saves >= 2 VB bytes
			//   (values with <= b+14 bits fit now (saving index + VB bytes),
			//    values with > b+14 bits keep a rest 14 bits shorter,
			//    i.e. 2 bytes shorter in our variable byte cost model).
			//   i.e. VB(b+14) <= VB(b) + 32*14 - 256*2
			//   i.e. VB(b+14) <= VB(b) + 448 - 512
			//   i.e. VB(b+14) <= VB(b)-64
			//
			// So VB(b) is never the minimum: nex <= 255 whenever we get here.
			// (Remainder blocks have <= 255 values, so nex is always <= 255.)
			bestType = blockBitpackingVBExceptions
			bestB = b
			best = size
		}
	}
	switch bestType {
	case blockBitpacking:
		return be.encodeBitpack(dest, vals, layout, bitWidth)
	case blockBitpackingExceptions:
		return be.encodeBitpackExc(dest, vals, layout, bestB, bitWidth-bestB)
	case blockBitpackingVBExceptions:
		return be.encodeBitpackVBExc(dest, vals, layout, bestB, int(stats.cnt[bestB]))
	default:
		panic("BUG: bestType not implemented")
	}
}

func (be *BlockEncoder) encodeBitpack(dest []byte, vals []uint32, layout blockLayout, bitWidth int) []byte {
	dest = append(dest, byte(bitWidth))
	if layout == interleaved {
		return bitpack256v(dest, vals, bitWidth)
	}
	return bitpack(dest, vals, bitWidth)
}

func (be *BlockEncoder) encodeBitpackExc(dest []byte, vals []uint32, layout blockLayout, bitWidth, bitWidthEx int) []byte {
	hdr := byte(bitWidth)
	hdr |= 1 << 7 // bitpacking with exceptions
	dest = append(dest, hdr, byte(bitWidthEx))
	clear(be.exmap[:])
	high := be.high[:0]
	for idx, val := range vals {
		if rest := val >> bitWidth; rest != 0 {
			be.exmap[idx/8] |= 1 << (idx % 8) // set bit in the exception bitmap
			high = append(high, rest)         // store high bits
		}
	}
	dest = append(dest, be.exmap[:(len(vals)+7)/8]...)
	dest = bitpack(dest, high, bitWidthEx)
	if layout == interleaved {
		return bitpack256v(dest, vals, bitWidth)
	}
	return bitpack(dest, vals, bitWidth)
}

func (be *BlockEncoder) encodeBitpackVBExc(dest []byte, vals []uint32, layout blockLayout, bitWidth, nex int) []byte {
	hdr := byte(bitWidth)
	hdr |= 1 << 6 // bitpacking with variable byte exceptions
	dest = append(dest, hdr, byte(nex))
	exidx := be.exidx[:cap(be.exidx)]
	high := be.high[:cap(be.high)]
	n := 0
	for idx, val := range vals {
		if rest := val >> bitWidth; rest != 0 {
			exidx[n] = byte(idx)
			high[n] = rest
			n++
		}
	}
	exidx = exidx[:n]
	high = high[:n]

	if layout == interleaved {
		dest = bitpack256v(dest, vals, bitWidth)
	} else {
		dest = bitpack(dest, vals, bitWidth)
	}
	for _, val := range high {
		dest = vbenc32(dest, val)
	}
	return append(dest, exidx...)
}

func (be *BlockEncoder) encodeConstant(dest []byte, vals []uint32, bitWidth int) []byte {
	hdr := byte(bitWidth)
	hdr |= 1<<7 | 1<<6 // constant block
	dest = append(dest, hdr)
	binary.LittleEndian.PutUint32(be.buf[:], vals[0])
	return append(dest, be.buf[:(bitWidth+7)/8]...)
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
