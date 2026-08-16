package pforenc

import (
	"encoding/binary"
	"slices"
)

func bitpack(dest []byte, vals []uint32, bitWidth int) []byte {
	if bitWidth == 0 {
		return dest // no payload, sparse block with only exceptions
	}
	if len(vals) >= 32 {
		size := 4 * bitWidth
		for len(vals) >= 32 {
			existing := len(dest)
			dest = slices.Grow(dest, size)[:existing+size]
			bitpack32(dest[existing:] /*append*/, (*[32]uint32)(vals), bitWidth)
			vals = vals[32:]
		}
	}
	mask := uint32(1<<bitWidth - 1)
	var acc uint64
	var have int
	for _, val := range vals {
		acc |= uint64(val&mask) << (uint(have) & 63)
		have += bitWidth
		for have >= 32 {
			dest = binary.LittleEndian.AppendUint32(dest, uint32(acc))
			acc >>= 32
			have -= 32
		}
	}
	for have > 0 {
		dest = append(dest, byte(acc))
		acc >>= 8
		have -= 8
	}
	return dest
}

// Go generics create one shape per bitpackUnrolled,
// i.e. in the resulting executable, we will see symbols like
// github.com/Debian/dcs/internal/turbopfor/pforenc.bitpackUnrolled[go.shape.[12]uint8]
//
// We use one type per bit width so that the compiler generates
// 32 versions of bitpackUnrolled, in each of which the bit width
// is known at compile time, which results in better code generation.

type bitWidthT interface {
	~[1]byte | ~[2]byte | ~[3]byte | ~[4]byte | ~[5]byte |
		~[6]byte | ~[7]byte | ~[8]byte | ~[9]byte | ~[10]byte |
		~[11]byte | ~[12]byte | ~[13]byte | ~[14]byte | ~[15]byte |
		~[16]byte | ~[17]byte | ~[18]byte | ~[19]byte | ~[20]byte |
		~[21]byte | ~[22]byte | ~[23]byte | ~[24]byte | ~[25]byte |
		~[26]byte | ~[27]byte | ~[28]byte | ~[29]byte | ~[30]byte |
		~[31]byte | ~[32]byte
}

func bitpack32(dest []byte, vals *[32]uint32, bitWidth int) {
	switch bitWidth {
	case 1:
		bitpack32Unrolled[[1]byte](dest, vals)
	case 2:
		bitpack32Unrolled[[2]byte](dest, vals)
	case 3:
		bitpack32Unrolled[[3]byte](dest, vals)
	case 4:
		bitpack32Unrolled[[4]byte](dest, vals)
	case 5:
		bitpack32Unrolled[[5]byte](dest, vals)
	case 6:
		bitpack32Unrolled[[6]byte](dest, vals)
	case 7:
		bitpack32Unrolled[[7]byte](dest, vals)
	case 8:
		bitpack32Unrolled[[8]byte](dest, vals)
	case 9:
		bitpack32Unrolled[[9]byte](dest, vals)
	case 10:
		bitpack32Unrolled[[10]byte](dest, vals)
	case 11:
		bitpack32Unrolled[[11]byte](dest, vals)
	case 12:
		bitpack32Unrolled[[12]byte](dest, vals)
	case 13:
		bitpack32Unrolled[[13]byte](dest, vals)
	case 14:
		bitpack32Unrolled[[14]byte](dest, vals)
	case 15:
		bitpack32Unrolled[[15]byte](dest, vals)
	case 16:
		bitpack32Unrolled[[16]byte](dest, vals)
	case 17:
		bitpack32Unrolled[[17]byte](dest, vals)
	case 18:
		bitpack32Unrolled[[18]byte](dest, vals)
	case 19:
		bitpack32Unrolled[[19]byte](dest, vals)
	case 20:
		bitpack32Unrolled[[20]byte](dest, vals)
	case 21:
		bitpack32Unrolled[[21]byte](dest, vals)
	case 22:
		bitpack32Unrolled[[22]byte](dest, vals)
	case 23:
		bitpack32Unrolled[[23]byte](dest, vals)
	case 24:
		bitpack32Unrolled[[24]byte](dest, vals)
	case 25:
		bitpack32Unrolled[[25]byte](dest, vals)
	case 26:
		bitpack32Unrolled[[26]byte](dest, vals)
	case 27:
		bitpack32Unrolled[[27]byte](dest, vals)
	case 28:
		bitpack32Unrolled[[28]byte](dest, vals)
	case 29:
		bitpack32Unrolled[[29]byte](dest, vals)
	case 30:
		bitpack32Unrolled[[30]byte](dest, vals)
	case 31:
		bitpack32Unrolled[[31]byte](dest, vals)
	case 32:
		bitpack32Unrolled[[32]byte](dest, vals)
	}
}

// bitpack32Unrolled is a manually unrolled version of bitpack at bitWidth n.
// Knowing the bit width at compile-time allows the compiler
// to generate much tighter code.
func bitpack32Unrolled[T bitWidthT](dest []byte, vals *[32]uint32) {
	var zero T
	bitWidth := len(zero)                  // known at compile time
	dest = dest[: 4*bitWidth : 4*bitWidth] // make cap known at compile time
	mask := uint32(1<<bitWidth - 1)

	var acc uint64
	var have, pos int

	// Manually unrolled loop starts here.
	// Each iteration is identical except for the vals[x] index.
	acc |= uint64(vals[0]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[1]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[2]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[3]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[4]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[5]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[6]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[7]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[8]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[9]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[10]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[11]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[12]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[13]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[14]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[15]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[16]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[17]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[18]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[19]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[20]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[21]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[22]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[23]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[24]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[25]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[26]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[27]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[28]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[29]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[30]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}

	acc |= uint64(vals[31]&mask) << have
	have += bitWidth
	if have >= 32 {
		binary.LittleEndian.PutUint32(dest[pos:pos+4], uint32(acc))
		pos += 4
		acc >>= 32
		have -= 32
	}
}
