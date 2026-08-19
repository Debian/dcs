package pfordec

import "encoding/binary"

// vbdec32 fills output from input, decoding variable byte uint32s.
//
// The variable byte encoding is similar to:
// https://sqlite.org/src4/doc/trunk/www/varint.wiki
// https://github.com/stoklund/varint
//
// [       0-       176] are stored in 1 byte (as-is)
// [     177-     16560] are stored in 2 bytes, with the highest 6 bits added to 177
// [   16561-    540848] are stored in 3 bytes, with the highest 3 bits added to 241
// [  540849-  16777215] are stored in 4 bytes, with 0 added to 249
// [16777216-4294967295] are stored in 5 bytes, with 1 added to 249
//
// An overflow marker will be used to signal that encoding the
// values would be less space-efficient than simply copying them
// (e.g. if all values require 5 bytes).
func vbdec32(input []byte, output []uint32) (read int) {
	before := len(input)
	if input[0] == 0xff {
		// overflow, memcpy the data as-is:
		input = input[1:]
		for op := range output {
			output[op] = binary.LittleEndian.Uint32(input)
			input = input[4:]
		}
		return before - len(input)
	}
	for op := range output {
		x := uint32(input[0])
		input = input[1:]
		if x < 177 {
		} else if x < 241 {
			x = uint32(input[0]) +
				((x - 177) << 8) +
				177
			input = input[1:]
		} else if x < 249 {
			x = (uint32(input[0]) << 0) +
				(uint32(input[1]) << 8) +
				((x - 241) << 16) +
				16561
			input = input[2:]
		} else {
			_b := x - 249 // _b in [0, 1]
			x = binary.LittleEndian.Uint32(input) & (((1 << (8 * _b)) << 24) - 1)
			input = input[3+_b:]
		}
		output[op] = x
	}
	return before - len(input)
}
