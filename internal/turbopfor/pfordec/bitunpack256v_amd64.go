//go:build goexperiment.simd && amd64

package pfordec

import "simd/archsimd"

func bitunpack256v32(fullinput []byte, fulloutput []uint32, nbits int) (read int) {
	if !hasAVX2 {
		return bitunpack256v32Scalar(fullinput, fulloutput, nbits)
	}
	output := (*[256]uint32)(fulloutput[:256])
	switch nbits {
	case 0:
		clear(output[:])
		return 0
	case 1:
		bitunpack256v32SIMDUnrolled[[1]byte](fullinput, output)
		return 32 * 1
	case 2:
		bitunpack256v32SIMDUnrolled[[2]byte](fullinput, output)
		return 32 * 2
	case 3:
		bitunpack256v32SIMDUnrolled[[3]byte](fullinput, output)
		return 32 * 3
	case 4:
		bitunpack256v32SIMDUnrolled[[4]byte](fullinput, output)
		return 32 * 4
	case 5:
		bitunpack256v32SIMDUnrolled[[5]byte](fullinput, output)
		return 32 * 5
	case 6:
		bitunpack256v32SIMDUnrolled[[6]byte](fullinput, output)
		return 32 * 6
	case 7:
		bitunpack256v32SIMDUnrolled[[7]byte](fullinput, output)
		return 32 * 7
	case 8:
		bitunpack256v32SIMDUnrolled[[8]byte](fullinput, output)
		return 32 * 8
	case 9:
		bitunpack256v32SIMDUnrolled[[9]byte](fullinput, output)
		return 32 * 9
	case 10:
		bitunpack256v32SIMDUnrolled[[10]byte](fullinput, output)
		return 32 * 10
	case 11:
		bitunpack256v32SIMDUnrolled[[11]byte](fullinput, output)
		return 32 * 11
	case 12:
		bitunpack256v32SIMDUnrolled[[12]byte](fullinput, output)
		return 32 * 12
	case 13:
		bitunpack256v32SIMDUnrolled[[13]byte](fullinput, output)
		return 32 * 13
	case 14:
		bitunpack256v32SIMDUnrolled[[14]byte](fullinput, output)
		return 32 * 14
	case 15:
		bitunpack256v32SIMDUnrolled[[15]byte](fullinput, output)
		return 32 * 15
	case 16:
		bitunpack256v32SIMDUnrolled[[16]byte](fullinput, output)
		return 32 * 16
	case 17:
		bitunpack256v32SIMDUnrolled[[17]byte](fullinput, output)
		return 32 * 17
	case 18:
		bitunpack256v32SIMDUnrolled[[18]byte](fullinput, output)
		return 32 * 18
	case 19:
		bitunpack256v32SIMDUnrolled[[19]byte](fullinput, output)
		return 32 * 19
	case 20:
		bitunpack256v32SIMDUnrolled[[20]byte](fullinput, output)
		return 32 * 20
	case 21:
		bitunpack256v32SIMDUnrolled[[21]byte](fullinput, output)
		return 32 * 21
	case 22:
		bitunpack256v32SIMDUnrolled[[22]byte](fullinput, output)
		return 32 * 22
	case 23:
		bitunpack256v32SIMDUnrolled[[23]byte](fullinput, output)
		return 32 * 23
	case 24:
		bitunpack256v32SIMDUnrolled[[24]byte](fullinput, output)
		return 32 * 24
	case 25:
		bitunpack256v32SIMDUnrolled[[25]byte](fullinput, output)
		return 32 * 25
	case 26:
		bitunpack256v32SIMDUnrolled[[26]byte](fullinput, output)
		return 32 * 26
	case 27:
		bitunpack256v32SIMDUnrolled[[27]byte](fullinput, output)
		return 32 * 27
	case 28:
		bitunpack256v32SIMDUnrolled[[28]byte](fullinput, output)
		return 32 * 28
	case 29:
		bitunpack256v32SIMDUnrolled[[29]byte](fullinput, output)
		return 32 * 29
	case 30:
		bitunpack256v32SIMDUnrolled[[30]byte](fullinput, output)
		return 32 * 30
	case 31:
		bitunpack256v32SIMDUnrolled[[31]byte](fullinput, output)
		return 32 * 31
	case 32:
		bitunpack256v32SIMDUnrolled[[32]byte](fullinput, output)
		return 32 * 32
	default:
		panic("invalid bitWidth")
	}
}

func bitunpack256v32SIMDUnrolled[T bitWidthT](input []byte, output *[256]uint32) {
	var zero T
	bitWidth := len(zero)                      // known at compile time
	input = input[: 32*bitWidth : 32*bitWidth] // make cap known at compile time
	mask8 := archsimd.BroadcastUint32x8(uint32(1)<<bitWidth - 1)

	row8 := loadRow(input, 0)
	row8 = bitunpack256v32SIMD1[T](input, output, 0, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 1, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 2, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 3, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 4, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 5, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 6, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 7, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 8, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 9, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 10, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 11, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 12, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 13, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 14, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 15, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 16, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 17, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 18, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 19, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 20, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 21, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 22, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 23, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 24, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 25, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 26, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 27, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 28, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 29, row8, mask8)
	row8 = bitunpack256v32SIMD1[T](input, output, 30, row8, mask8)
	bitunpack256v32SIMD1[T](input, output, 31, row8, mask8)
}

func loadRow(input []byte, row int) archsimd.Uint32x8 {
	return archsimd.LoadUint8x32(input[32*row : 32*row+32]).ReshapeToUint32s()
}

func bitunpack256v32SIMD1[T bitWidthT](input []byte, output *[256]uint32, g int, row8, mask8 archsimd.Uint32x8) archsimd.Uint32x8 {
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
	vals8.And(mask8).Store(output[g*8 : g*8+8])
	return row8
}
