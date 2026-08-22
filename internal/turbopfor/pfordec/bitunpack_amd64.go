//go:build goexperiment.simd && amd64

package pfordec

import (
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
