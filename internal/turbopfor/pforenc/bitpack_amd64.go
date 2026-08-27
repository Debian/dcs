//go:build goexperiment.simd && amd64

package pforenc

import (
	"encoding/binary"
	"math/bits"
	"simd/archsimd"
	"slices"

	"github.com/Debian/dcs/internal/turbopfor"
)

func bitpack256v(dest []byte, vals []uint32, bitWidth int) []byte {
	if !turbopfor.HasAVX2 {
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

func exbitmap(vals []uint32, bitWidth int, exmap []byte, high []uint32) int {
	if !turbopfor.HasAVX512 {
		return exbitmapScalar(vals, bitWidth, exmap, high)
	}
	fits := archsimd.BroadcastUint32x16(uint32(1)<<bitWidth - 1)
	nex := 0
	idx := 0
	for ; idx+16 <= len(vals); idx += 16 {
		vals16 := archsimd.LoadUint32x16(vals[idx : idx+16])
		mask16 := vals16.Greater(fits) // if val >> bitWidth != 0
		flags := mask16.ToBits()       // turn mask values into bitmap
		binary.LittleEndian.PutUint16(exmap[idx/8:idx/8+2], flags)
		vals16 = vals16.ShiftAllRight(uint64(bitWidth)) // val >> bitWidth
		vals16 = vals16.Compress(mask16)                // pack the masked elements
		vals16.Store(high[nex : nex+16])                // store the high bits
		nex += bits.OnesCount16(flags)
	}
	high = high[:nex]
	for i, val := range vals[idx:] {
		if rest := val >> bitWidth; rest != 0 {
			exmap[(idx+i)/8] |= 1 << ((idx + i) % 8) // set bit in the exception bitmap
			high = append(high, rest)                // store high bytes
		}
	}
	return len(high)
}
