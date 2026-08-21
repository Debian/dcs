package pfordec

import "encoding/binary"

// bitpacking 7 bit uses little endian:
// 1 0101010   10 011100
// n current   nn current
//
// → 0101010, 0111001, xxxxx10, …
//
// longer values:
//
//	1120345    (100010001100001011001)
//	                         01011001
//	                 00011000
//	        101 10001
//
// 00001011
func bitunpackScalar(input []byte, output []uint32, bitWidth int) (read int) {
	if bitWidth == 0 {
		clear(output)
		return 0
	}
	orig := len(input)
	for len(output) >= 32 {
		bitunpack32(input, (*[32]uint32)(output), bitWidth)
		input = input[4*bitWidth:]
		output = output[32:]
	}
	var have int   // remaining bits
	var acc uint64 // accumulator
	for op := 0; op < len(output); {
		if have < bitWidth {
			// shift in one more byte
			acc |= uint64(input[0]) << have
			input = input[1:]
			have += 8
		}
		if have >= bitWidth {
			output[op] = uint32(acc & ((1 << bitWidth) - 1))
			op++
			acc >>= bitWidth
			have -= bitWidth
		}
	}
	return orig - len(input)
}

func bitunpack256v32Scalar(fullinput []byte, fulloutput []uint32, bitWidth int) (read int) {
	output := fulloutput[:256]
	if bitWidth == 0 {
		clear(output)
		return 0
	}
	mask := uint64(1)<<bitWidth - 1
	n := 32 * int(bitWidth)
	input := fullinput[:n] // tell the Go compiler how long the input is
	var have uint
	var acc [8]uint64 // accumulator
	pos := 0
	for op := 0; op < 256; op += 8 {
		if have < uint(bitWidth) {
			// read 8 more uint32s
			row := input[pos : pos+32]
			acc[0] |= uint64(binary.LittleEndian.Uint32(row[0:])) << have
			acc[1] |= uint64(binary.LittleEndian.Uint32(row[4:])) << have
			acc[2] |= uint64(binary.LittleEndian.Uint32(row[8:])) << have
			acc[3] |= uint64(binary.LittleEndian.Uint32(row[12:])) << have
			acc[4] |= uint64(binary.LittleEndian.Uint32(row[16:])) << have
			acc[5] |= uint64(binary.LittleEndian.Uint32(row[20:])) << have
			acc[6] |= uint64(binary.LittleEndian.Uint32(row[24:])) << have
			acc[7] |= uint64(binary.LittleEndian.Uint32(row[28:])) << have
			have += 32
			pos += 32
		}
		out8 := output[op : op+8]
		out8[0] = uint32(acc[0] & mask)
		acc[0] >>= bitWidth
		out8[1] = uint32(acc[1] & mask)
		acc[1] >>= bitWidth
		out8[2] = uint32(acc[2] & mask)
		acc[2] >>= bitWidth
		out8[3] = uint32(acc[3] & mask)
		acc[3] >>= bitWidth
		out8[4] = uint32(acc[4] & mask)
		acc[4] >>= bitWidth
		out8[5] = uint32(acc[5] & mask)
		acc[5] >>= bitWidth
		out8[6] = uint32(acc[6] & mask)
		acc[6] >>= bitWidth
		out8[7] = uint32(acc[7] & mask)
		acc[7] >>= bitWidth
		have -= uint(bitWidth)
	}
	return n
}
