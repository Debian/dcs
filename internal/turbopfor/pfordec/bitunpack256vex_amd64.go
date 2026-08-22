//go:build goexperiment.simd && amd64

package pfordec

import (
	"math/bits"
	"simd/archsimd"
)

// bitunpack256v32Ex is like bitunpack256v32, but with exception decoding fused
// into the same loop (instead of a separate pass).
func bitunpack256v32Ex(input []byte, fulloutput []uint32, nbits int, exmap *[32]byte, exceptions *[256]uint32) (read int) {
	if !hasAVX512 {
		return bitunpack256v32ExScalar(input, fulloutput, nbits, exmap, exceptions)
	}
	output := (*[256]uint32)(fulloutput[:256])
	switch nbits {
	case 0:
		j := 0
		for g := range 32 {
			m := exmap[g]
			gatherExceptions(exceptions, j, m).Store(output[g*8 : g*8+8])
			// Go compiles OnesCount32 into an intrinsic,
			// but not OnesCount8, so we convert to uint32:
			j += bits.OnesCount32(uint32(m))
		}
		return 0
	case 1:
		bitunpack256v32ExSIMDUnrolled[[1]byte](input, output, exmap, exceptions)
		return 32 * 1
	case 2:
		bitunpack256v32ExSIMDUnrolled[[2]byte](input, output, exmap, exceptions)
		return 32 * 2
	case 3:
		bitunpack256v32ExSIMDUnrolled[[3]byte](input, output, exmap, exceptions)
		return 32 * 3
	case 4:
		bitunpack256v32ExSIMDUnrolled[[4]byte](input, output, exmap, exceptions)
		return 32 * 4
	case 5:
		bitunpack256v32ExSIMDUnrolled[[5]byte](input, output, exmap, exceptions)
		return 32 * 5
	case 6:
		bitunpack256v32ExSIMDUnrolled[[6]byte](input, output, exmap, exceptions)
		return 32 * 6
	case 7:
		bitunpack256v32ExSIMDUnrolled[[7]byte](input, output, exmap, exceptions)
		return 32 * 7
	case 8:
		bitunpack256v32ExSIMDUnrolled[[8]byte](input, output, exmap, exceptions)
		return 32 * 8
	case 9:
		bitunpack256v32ExSIMDUnrolled[[9]byte](input, output, exmap, exceptions)
		return 32 * 9
	case 10:
		bitunpack256v32ExSIMDUnrolled[[10]byte](input, output, exmap, exceptions)
		return 32 * 10
	case 11:
		bitunpack256v32ExSIMDUnrolled[[11]byte](input, output, exmap, exceptions)
		return 32 * 11
	case 12:
		bitunpack256v32ExSIMDUnrolled[[12]byte](input, output, exmap, exceptions)
		return 32 * 12
	case 13:
		bitunpack256v32ExSIMDUnrolled[[13]byte](input, output, exmap, exceptions)
		return 32 * 13
	case 14:
		bitunpack256v32ExSIMDUnrolled[[14]byte](input, output, exmap, exceptions)
		return 32 * 14
	case 15:
		bitunpack256v32ExSIMDUnrolled[[15]byte](input, output, exmap, exceptions)
		return 32 * 15
	case 16:
		bitunpack256v32ExSIMDUnrolled[[16]byte](input, output, exmap, exceptions)
		return 32 * 16
	case 17:
		bitunpack256v32ExSIMDUnrolled[[17]byte](input, output, exmap, exceptions)
		return 32 * 17
	case 18:
		bitunpack256v32ExSIMDUnrolled[[18]byte](input, output, exmap, exceptions)
		return 32 * 18
	case 19:
		bitunpack256v32ExSIMDUnrolled[[19]byte](input, output, exmap, exceptions)
		return 32 * 19
	case 20:
		bitunpack256v32ExSIMDUnrolled[[20]byte](input, output, exmap, exceptions)
		return 32 * 20
	case 21:
		bitunpack256v32ExSIMDUnrolled[[21]byte](input, output, exmap, exceptions)
		return 32 * 21
	case 22:
		bitunpack256v32ExSIMDUnrolled[[22]byte](input, output, exmap, exceptions)
		return 32 * 22
	case 23:
		bitunpack256v32ExSIMDUnrolled[[23]byte](input, output, exmap, exceptions)
		return 32 * 23
	case 24:
		bitunpack256v32ExSIMDUnrolled[[24]byte](input, output, exmap, exceptions)
		return 32 * 24
	case 25:
		bitunpack256v32ExSIMDUnrolled[[25]byte](input, output, exmap, exceptions)
		return 32 * 25
	case 26:
		bitunpack256v32ExSIMDUnrolled[[26]byte](input, output, exmap, exceptions)
		return 32 * 26
	case 27:
		bitunpack256v32ExSIMDUnrolled[[27]byte](input, output, exmap, exceptions)
		return 32 * 27
	case 28:
		bitunpack256v32ExSIMDUnrolled[[28]byte](input, output, exmap, exceptions)
		return 32 * 28
	case 29:
		bitunpack256v32ExSIMDUnrolled[[29]byte](input, output, exmap, exceptions)
		return 32 * 29
	case 30:
		bitunpack256v32ExSIMDUnrolled[[30]byte](input, output, exmap, exceptions)
		return 32 * 30
	case 31:
		bitunpack256v32ExSIMDUnrolled[[31]byte](input, output, exmap, exceptions)
		return 32 * 31
	case 32:
		bitunpack256v32ExSIMDUnrolled[[32]byte](input, output, exmap, exceptions)
		return 32 * 32
	default:
		panic("invalid bitWidth")
	}
}

// gatherExceptions loads 8 exceptions at offset j, per bitmask m.
func gatherExceptions(exceptions *[256]uint32, j int, m byte) archsimd.Uint32x8 {
	// VPEXPANDD has a false dependency on Zen 4 and Zen 5 (at least).
	// Xor(zero) is a workaround; the Go compiler should do this.
	var zero archsimd.Uint32x8
	return archsimd.LoadUint32x8Array((*[8]uint32)(exceptions[j : j+8])).Xor(zero).Expand(archsimd.Mask32x8FromBits(m))
}

func bitunpack256v32ExSIMDUnrolled[T bitWidthT](input []byte, output *[256]uint32, exmap *[32]byte, exceptions *[256]uint32) {
	var zero T
	bitWidth := len(zero)                      // known at compile time
	input = input[: 32*bitWidth : 32*bitWidth] // make cap known at compile time
	mask8 := archsimd.BroadcastUint32x8(uint32(1)<<bitWidth - 1)
	row8 := loadRow(input, 0)
	j := 0
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 0, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 1, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 2, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 3, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 4, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 5, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 6, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 7, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 8, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 9, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 10, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 11, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 12, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 13, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 14, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 15, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 16, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 17, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 18, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 19, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 20, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 21, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 22, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 23, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 24, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 25, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 26, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 27, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 28, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 29, row8, mask8, exmap, exceptions, j)
	row8, j = bitunpack256v32ExSIMD1[T](input, output, 30, row8, mask8, exmap, exceptions, j)
	bitunpack256v32ExSIMD1[T](input, output, 31, row8, mask8, exmap, exceptions, j)
}

func bitunpack256v32ExSIMD1[T bitWidthT](input []byte, output *[256]uint32, g int, row8, mask8 archsimd.Uint32x8, exmap *[32]byte, exceptions *[256]uint32, j int) (archsimd.Uint32x8, int) {
	var zero T
	bitWidth := len(zero) // known at compile time
	bit := g * bitWidth
	row := bit / 32
	offset := bit % 32
	vals8 := row8.ShiftAllRight(uint64(offset)) // vals8 = row8 >> off
	if offset+bitWidth > 32 {
		// Load the low bits from the next row.
		row8 = loadRow(input, row+1)
		vals8 = vals8.Or(row8.ShiftAllLeft(uint64(32 - offset)))
	} else if offset+bitWidth == 32 && row+1 < bitWidth {
		// Current row fully processed, load the next one.
		row8 = loadRow(input, row+1)
	}
	// the following lines are no longer identical to bitunpack256v32SIMD1:
	// this function also does exception decoding.
	m := exmap[g]
	vals8 = vals8.And(mask8).Or(gatherExceptions(exceptions, j, m).ShiftAllLeft(uint64(bitWidth)))
	vals8.Store(output[g*8 : g*8+8])
	return row8, j + bits.OnesCount32(uint32(m))
}
