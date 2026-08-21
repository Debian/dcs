// Package pfordec contains a Go TurboPFor decoder.
package pfordec

import (
	"encoding/binary"
	"math/bits"
)

type blockLayout int

const (
	interleaved blockLayout = iota // full block with interleaved layout (256v)
	sequential                     // remainder block
)

// BlockDecoder decodes one or more TurboPFor blocks, each containing
// at most 256 values (for efficient SIMD processing).
//
// A zero value BlockDecoder is ready to use.
// BlockDecoder is not safe for concurrent use by multiple goroutines.
type BlockDecoder struct {
	scratch [256]uint32
}

// DecodeN decodes len(output) uint32s from input to output.
func (bd *BlockDecoder) DecodeN(input []byte, output []uint32) (read int) {
	n := len(output)
	for n >= 256 {
		blockRead := bd.decodeFull(input, output[:256])
		input = input[blockRead:]
		output = output[256:]
		read += blockRead
		n -= 256
	}
	if n > 0 {
		blockRead := bd.DecodeBlock(input, output[:n])
		input = input[blockRead:]
		read += blockRead
	}
	return read
}

// DecodeBlock decodes len(output)<=256 uint32s from input to output
// (one TurboPFor block).
func (bd *BlockDecoder) DecodeBlock(input []byte, output []uint32) (read int) {
	if len(output) < 256 {
		return bd.decodeRemainder(input, output)
	}
	return bd.decodeFull(input, output)
}

func (bd *BlockDecoder) decodeFull(input []byte, output []uint32) (read int) {
	return bd.decode(input, output, interleaved)
}

func (bd *BlockDecoder) decodeRemainder(input []byte, output []uint32) (read int) {
	return bd.decode(input, output, sequential)
}

type blockType int

const (
	// bitpacked values (no exceptions)
	blockBitpacking blockType = iota
	// variable byte encoded exception values and exception index bytes
	blockBitpackingVBExceptions
	// exception presence bitmap + bitpacked exception values
	blockBitpackingExceptions
	// constant value for entire block
	blockConstant
)

// decode decodes one block of TurboPFor-encoded uint32s.
func (bd *BlockDecoder) decode(input []byte, output []uint32, layout blockLayout) (read int) {
	if len(output) == 0 {
		return 0
	}
	before := len(input)            // for returning read bytes
	b, input := input[0], input[1:] // block header
	bitWidth := int(b & ^byte(0x80|0x40))
	switch blockType(b >> 6) {
	case blockConstant:
		var buf [4]byte
		copy(buf[:], input)
		u := binary.LittleEndian.Uint32(buf[:])
		if bitWidth < 32 {
			u &= ((1 << bitWidth) - 1)
		}
		fillConstant(output, u)
		return 1 + (int(bitWidth)+7)/8

	case blockBitpacking:
		if layout == interleaved {
			return 1 + bitunpack256v32(input, output, bitWidth)
		}
		return 1 + bitunpack(input, output, int(bitWidth))

	case blockBitpackingExceptions:
		bx, input := input[0], input[1:]
		n := len(output)

		exmap := input
		nex := 0 // number of exceptions
		i := 0
		for ; i+8 <= n/8; i += 8 {
			xm8 := binary.LittleEndian.Uint64(exmap[i:])
			nex += bits.OnesCount64(xm8)
		}
		for ; i < (n+7)/8; i++ {
			xmb := exmap[i]
			if rem := n - i*8; rem < 8 {
				xmb &= 1<<rem - 1
			}
			// Go compiles OnesCount32 into an intrinsic,
			// but not OnesCount8, so we convert to uint32:
			nex += bits.OnesCount32(uint32(xmb))
		}
		input = input[(n+7)/8:]

		exceptions := bd.scratch[:nex]
		input = input[bitunpack(input, exceptions, int(bx)):]
		if layout == interleaved {
			input = input[bitunpack256v32Ex(input, output, bitWidth, (*[32]byte)(exmap), &bd.scratch):]
		} else {
			input = input[bitunpack(input, output, int(bitWidth)):]
			applyBitmapExceptionsScalar(output, exmap, exceptions, bitWidth)
		}

		return before - len(input)

	default: // blockBitpackingVBExceptions
		nex, input := int(input[0]), input[1:] // number of exceptions
		if layout == interleaved {
			input = input[bitunpack256v32(input, output, bitWidth):]
		} else {
			input = input[bitunpack(input, output, int(bitWidth)):]
		}

		exceptions := bd.scratch[:nex]
		input = input[vbdec32(input, exceptions):]
		for i := range nex {
			output[input[i]] |= exceptions[i] << bitWidth
		}
		return before - len(input) + nex
	}
}

// StreamDecoder decodes one full TurboPFor block at a time.
//
// A zero value StreamDecoder is ready to use.
// StreamDecoder is not safe for concurrent use by multiple goroutines.
type StreamDecoder struct {
	bd   BlockDecoder
	vals [256]uint32
}

// DecodeBlock decodes one TurboPFor block and returns the decoded values.
//
// Contract: the returned values are valid until the next DecodeBlock.
func (sd *StreamDecoder) DecodeBlock(input []byte, n int) (vals []uint32, read int) {
	output := sd.vals[:n]
	return output, sd.bd.DecodeBlock(input, output)
}
