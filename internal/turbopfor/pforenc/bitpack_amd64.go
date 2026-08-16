//go:build goexperiment.simd && amd64

package pforenc

import (
	"simd/archsimd"
	"slices"
)

var hasAVX2 = archsimd.X86.AVX2()

func bitpack256v(dest []byte, vals []uint32, bitWidth int) []byte {
	if !hasAVX2 {
		return bitpack256vScalar(dest, vals, bitWidth)
	}
	size := 32 * bitWidth
	existing := len(dest)
	dest = slices.Grow(dest, size)[:existing+size]
	bitpack256vInPlace(dest[existing:] /* append */, vals, bitWidth)
	return dest
}

func bitpack256vInPlace(dest []byte, vals []uint32, bitWidth int) {
	mask := archsimd.BroadcastUint32x8(uint32(1)<<bitWidth - 1)
	zero := archsimd.BroadcastUint32x8(0)
	acc := zero
	pos := uint64(0)
	row := 0
	for g := 0; g < 256; g += 8 {
		vals8 := archsimd.LoadUint32x8(vals[g : g+8])
		vals8 = vals8.And(mask)               // val &= mask
		acc = acc.Or(vals8.ShiftAllLeft(pos)) // acc |= val << pos
		pos += uint64(bitWidth)
		if pos < 32 {
			continue // keep filling the accumulator
		}
		// write 8 uint32s to dest
		acc.ReshapeToUint8s().Store(dest[row*32 : row*32+32])
		row++
		pos -= 32
		if pos > 0 {
			acc = vals8.ShiftAllRight(uint64(bitWidth) - pos)
		} else {
			acc = zero
		}
	}
}
