package pfordec

import "encoding/binary"

type bitWidthT interface {
	~[1]byte | ~[2]byte | ~[3]byte | ~[4]byte | ~[5]byte |
		~[6]byte | ~[7]byte | ~[8]byte | ~[9]byte | ~[10]byte |
		~[11]byte | ~[12]byte | ~[13]byte | ~[14]byte | ~[15]byte |
		~[16]byte | ~[17]byte | ~[18]byte | ~[19]byte | ~[20]byte |
		~[21]byte | ~[22]byte | ~[23]byte | ~[24]byte | ~[25]byte |
		~[26]byte | ~[27]byte | ~[28]byte | ~[29]byte | ~[30]byte |
		~[31]byte | ~[32]byte
}

func bitunpack32(input []byte, output *[32]uint32, bitWidth int) {
	switch bitWidth {
	case 1:
		bitunpack32Unrolled[[1]byte](input, output)
	case 2:
		bitunpack32Unrolled[[2]byte](input, output)
	case 3:
		bitunpack32Unrolled[[3]byte](input, output)
	case 4:
		bitunpack32Unrolled[[4]byte](input, output)
	case 5:
		bitunpack32Unrolled[[5]byte](input, output)
	case 6:
		bitunpack32Unrolled[[6]byte](input, output)
	case 7:
		bitunpack32Unrolled[[7]byte](input, output)
	case 8:
		bitunpack32Unrolled[[8]byte](input, output)
	case 9:
		bitunpack32Unrolled[[9]byte](input, output)
	case 10:
		bitunpack32Unrolled[[10]byte](input, output)
	case 11:
		bitunpack32Unrolled[[11]byte](input, output)
	case 12:
		bitunpack32Unrolled[[12]byte](input, output)
	case 13:
		bitunpack32Unrolled[[13]byte](input, output)
	case 14:
		bitunpack32Unrolled[[14]byte](input, output)
	case 15:
		bitunpack32Unrolled[[15]byte](input, output)
	case 16:
		bitunpack32Unrolled[[16]byte](input, output)
	case 17:
		bitunpack32Unrolled[[17]byte](input, output)
	case 18:
		bitunpack32Unrolled[[18]byte](input, output)
	case 19:
		bitunpack32Unrolled[[19]byte](input, output)
	case 20:
		bitunpack32Unrolled[[20]byte](input, output)
	case 21:
		bitunpack32Unrolled[[21]byte](input, output)
	case 22:
		bitunpack32Unrolled[[22]byte](input, output)
	case 23:
		bitunpack32Unrolled[[23]byte](input, output)
	case 24:
		bitunpack32Unrolled[[24]byte](input, output)
	case 25:
		bitunpack32Unrolled[[25]byte](input, output)
	case 26:
		bitunpack32Unrolled[[26]byte](input, output)
	case 27:
		bitunpack32Unrolled[[27]byte](input, output)
	case 28:
		bitunpack32Unrolled[[28]byte](input, output)
	case 29:
		bitunpack32Unrolled[[29]byte](input, output)
	case 30:
		bitunpack32Unrolled[[30]byte](input, output)
	case 31:
		bitunpack32Unrolled[[31]byte](input, output)
	case 32:
		bitunpack32Unrolled[[32]byte](input, output)
	}
}

// bitunpack32Unrolled is a manually unrolled version of bitunpack32 at bitWidth n.
func bitunpack32Unrolled[T bitWidthT](input []byte, output *[32]uint32) {
	var zero T
	bitWidth := len(zero)                    // known at compile time
	input = input[: 4*bitWidth : 4*bitWidth] // make cap known at compile time
	mask := uint32(1<<bitWidth - 1)

	output[0] = bitunpack1(input, 0*bitWidth, bitWidth, mask)
	output[1] = bitunpack1(input, 1*bitWidth, bitWidth, mask)
	output[2] = bitunpack1(input, 2*bitWidth, bitWidth, mask)
	output[3] = bitunpack1(input, 3*bitWidth, bitWidth, mask)
	output[4] = bitunpack1(input, 4*bitWidth, bitWidth, mask)
	output[5] = bitunpack1(input, 5*bitWidth, bitWidth, mask)
	output[6] = bitunpack1(input, 6*bitWidth, bitWidth, mask)
	output[7] = bitunpack1(input, 7*bitWidth, bitWidth, mask)
	output[8] = bitunpack1(input, 8*bitWidth, bitWidth, mask)
	output[9] = bitunpack1(input, 9*bitWidth, bitWidth, mask)
	output[10] = bitunpack1(input, 10*bitWidth, bitWidth, mask)
	output[11] = bitunpack1(input, 11*bitWidth, bitWidth, mask)
	output[12] = bitunpack1(input, 12*bitWidth, bitWidth, mask)
	output[13] = bitunpack1(input, 13*bitWidth, bitWidth, mask)
	output[14] = bitunpack1(input, 14*bitWidth, bitWidth, mask)
	output[15] = bitunpack1(input, 15*bitWidth, bitWidth, mask)
	output[16] = bitunpack1(input, 16*bitWidth, bitWidth, mask)
	output[17] = bitunpack1(input, 17*bitWidth, bitWidth, mask)
	output[18] = bitunpack1(input, 18*bitWidth, bitWidth, mask)
	output[19] = bitunpack1(input, 19*bitWidth, bitWidth, mask)
	output[20] = bitunpack1(input, 20*bitWidth, bitWidth, mask)
	output[21] = bitunpack1(input, 21*bitWidth, bitWidth, mask)
	output[22] = bitunpack1(input, 22*bitWidth, bitWidth, mask)
	output[23] = bitunpack1(input, 23*bitWidth, bitWidth, mask)
	output[24] = bitunpack1(input, 24*bitWidth, bitWidth, mask)
	output[25] = bitunpack1(input, 25*bitWidth, bitWidth, mask)
	output[26] = bitunpack1(input, 26*bitWidth, bitWidth, mask)
	output[27] = bitunpack1(input, 27*bitWidth, bitWidth, mask)
	output[28] = bitunpack1(input, 28*bitWidth, bitWidth, mask)
	output[29] = bitunpack1(input, 29*bitWidth, bitWidth, mask)
	output[30] = bitunpack1(input, 30*bitWidth, bitWidth, mask)
	output[31] = bitunpack1(input, 31*bitWidth, bitWidth, mask)
}

func bitunpack1(input []byte, offset, bitWidth int, mask uint32) uint32 {
	word := offset / 32 * 4 // byte offset of the uint32 with the low bits
	shift := offset % 32
	if shift+bitWidth <= 32 {
		// bitpacked value fits into one uint32
		return binary.LittleEndian.Uint32(input[word:word+4]) >> shift & mask
	}
	// bitpacked value needs two uint32s
	return uint32(binary.LittleEndian.Uint64(input[word:word+8])>>shift) & mask
}
