//go:build goexperiment.simd && amd64

package pfordec

import (
	"math/bits"
	"simd/archsimd"
)

var hasAVX512 = archsimd.X86.AVX512()

// seqUnpackConsts are the per-lane constants of bitunpackSIMD for one
// bit width. This allows keeping the dispatch unit busy with vector
// instructions, while loading all constants from L1 cache.
type seqUnpackConsts struct {
	start, offset, next, shl [8]uint32
}

var precomputed = func() (t [33]seqUnpackConsts) {
	for bitWidth := 1; bitWidth <= 32; bitWidth++ {
		c := &t[bitWidth]
		// Our SIMD registers contain 8 uint32s.
		for i := range 8 {
			// value i occupies bits [i*bitWidth, i*bitWidth+bitWidth)
			bit := i * bitWidth
			c.start[i] = uint32(bit / 32)        // which dword the value starts in
			c.offset[i] = uint32(bit % 32)       // bit offset within that dword
			c.next[i] = min(uint32(bit/32+1), 7) // where the value continues if it crosses the boundary
			c.shl[i] = uint32(32 - bit%32)       // how for up the continuation bits belong
		}
	}
	return t
}()

func bitunpack(input []byte, output []uint32, bitWidth int) (read int) {
	if !hasAVX2 {
		return bitunpackScalar(input, output, bitWidth)
	}
	if bitWidth == 0 {
		clear(output)
		return 0
	}
	c := &precomputed[bitWidth]
	start := archsimd.LoadUint32x8Array(&c.start)
	next := archsimd.LoadUint32x8Array(&c.next)
	offset := archsimd.LoadUint32x8Array(&c.offset)
	shl := archsimd.LoadUint32x8Array(&c.shl)

	mask8 := archsimd.BroadcastUint32x8(uint32(1)<<bitWidth - 1)
	total := (len(output)*bitWidth + 7) / 8
	pos := 0
	op := 0
	for op+8 <= len(output) && pos+32 <= len(input) {
		v32 := archsimd.LoadUint8x32Array((*[32]uint8)(input[pos : pos+32])).ReshapeToUint32s()
		bitunpackSIMD8(v32, start, next, offset, shl, mask8).StoreArray((*[8]uint32)(output[op : op+8]))
		op += 8
		pos += bitWidth
	}
	if op == len(output) {
		return total // exit early, without copying data
	}
	output = output[op:]
	var padded [64]byte
	copy(padded[:], input[pos:total])
	pos = 0
	for len(output) >= 8 {
		v32 := archsimd.LoadUint8x32Array((*[32]uint8)(padded[pos : pos+32])).ReshapeToUint32s()
		bitunpackSIMD8(v32, start, next, offset, shl, mask8).StoreArray((*[8]uint32)(output[:8]))
		pos += bitWidth
		output = output[8:]
	}
	if len(output) > 0 {
		var vals [8]uint32
		v32 := archsimd.LoadUint8x32Array((*[32]uint8)(padded[pos : pos+32])).ReshapeToUint32s()
		bitunpackSIMD8(v32, start, next, offset, shl, mask8).StoreArray(&vals)
		copy(output, vals[:])
	}
	return total
}

func bitunpackSIMD8(v32, start, next, offset, shl, mask8 archsimd.Uint32x8) archsimd.Uint32x8 {
	acc8 := v32.Permute(start)     // acc = dword holding lane i's low bits
	acc8 = acc8.ShiftRight(offset) // acc >>= bits consumed by lanes 0..i-1 (the rbits bookkeeping)
	next8 := v32.Permute(next)     // next dword: the refill (the scalar's input[0], a byte at a time)
	next8 = next8.ShiftLeft(shl)   // its bits belong above the 32-shr bits still in acc
	acc8 = acc8.Or(next8)          // acc |= next << rbits
	return acc8.And(mask8)         // output[op+i] = acc & (1<<bitWidth - 1)
}

func bitunpack256v32(fullinput []byte, fulloutput []uint32, nbits int) (read int) {
	if !hasAVX2 {
		return bitunpack256v32Scalar(fullinput, fulloutput, nbits)
	}
	output := fulloutput[:256]
	if nbits == 0 {
		clear(output)
		return 0
	}
	n := 32 * int(nbits)
	input := fullinput[:n] // tell the Go compiler how long the input is
	mask8 := archsimd.BroadcastUint32x8(uint32(1)<<nbits - 1)
	acc8 := archsimd.LoadUint8x32(input[0:32]).ReshapeToUint32s()
	have := 32
	pos := 32
	for op := 0; op < 256; op += 8 {
		vals8 := acc8
		if have < int(nbits) {
			// read 8 more uint32s
			next := archsimd.LoadUint8x32(input[pos : pos+32]).ReshapeToUint32s()
			pos += 32
			vals8 = acc8.Or(next.ShiftAllLeft(uint64(have)))
			acc8 = next.ShiftAllRight(uint64(int(nbits) - have))
			have += 32
		} else {
			acc8 = acc8.ShiftAllRight(uint64(nbits))
		}
		have -= int(nbits)
		vals8.And(mask8).Store(output[op : op+8])
	}
	return n
}

// bitunpack256v32Ex is like bitunpack256v32, but with exception decoding fused
// into the same loop (instead of a separate pass).
func bitunpack256v32Ex(fullinput []byte, fulloutput []uint32, nbits int, exmap *[32]byte, exceptions *[256]uint32) (read int) {
	if hasAVX512 {
		return bitunpack256v32ExSIMD(fullinput, fulloutput, nbits, exmap, exceptions)
	}
	return bitunpack256v32ExScalar(fullinput, fulloutput, nbits, exmap, exceptions)
}

func bitunpack256v32ExSIMD(fullinput []byte, fulloutput []uint32, nbits int, exmap *[32]byte, exceptions *[256]uint32) (read int) {
	output := fulloutput[:256]
	if nbits == 0 {
		j := 0
		for g := range 32 {
			m := exmap[g]
			gatherExceptions(exceptions, j, m).Store(output[g*8 : g*8+8])
			// Go compiles OnesCount32 into an intrinsic,
			// but not OnesCount8, so we convert to uint32:
			j += bits.OnesCount32(uint32(m))
		}
		return 0
	}
	n := 32 * int(nbits)
	input := fullinput[:n] // tell the Go compiler how long the input is
	mask8 := archsimd.BroadcastUint32x8(uint32(1)<<nbits - 1)
	acc8 := archsimd.LoadUint8x32(input[0:32]).ReshapeToUint32s()
	have := 32
	pos := 32
	j := 0
	for op := 0; op < 256; op += 8 {
		vals8 := acc8
		if have < int(nbits) {
			// read 8 more uint32s
			next := archsimd.LoadUint8x32(input[pos : pos+32]).ReshapeToUint32s()
			pos += 32
			vals8 = acc8.Or(next.ShiftAllLeft(uint64(have)))
			acc8 = next.ShiftAllRight(uint64(int(nbits) - have))
			have += 32
		} else {
			acc8 = acc8.ShiftAllRight(uint64(nbits))
		}
		have -= int(nbits)
		// the following lines are no longer identical to bitunpack256v32SIMD:
		// this function also does exception decoding.
		m := exmap[op/8]
		vals8.And(mask8).Or(gatherExceptions(exceptions, j, m).ShiftAllLeft(uint64(nbits))).Store(output[op : op+8])
		// Go compiles OnesCount32 into an intrinsic,
		// but not OnesCount8, so we convert to uint32:
		j += bits.OnesCount32(uint32(m))
	}
	return n
}

// gatherExceptions loads 8 exceptions at offset j, per bitmask m.
func gatherExceptions(exceptions *[256]uint32, j int, m byte) archsimd.Uint32x8 {
	// VPEXPANDD has a false dependency on Zen 4 and Zen 5 (at least).
	// Xor(zero) is a workaround; the Go compiler should do this.
	var zero archsimd.Uint32x8
	return archsimd.LoadUint32x8Array((*[8]uint32)(exceptions[j : j+8])).Xor(zero).Expand(archsimd.Mask32x8FromBits(m))
}
