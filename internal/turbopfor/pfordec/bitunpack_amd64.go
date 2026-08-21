//go:build goexperiment.simd && amd64

package pfordec

import (
	"simd/archsimd"
)

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
	bits := 32
	pos := 32
	for op := 0; op < 256; op += 8 {
		vals8 := acc8
		if bits < int(nbits) {
			// read 8 more uint32s
			next := archsimd.LoadUint8x32(input[pos : pos+32]).ReshapeToUint32s()
			pos += 32
			vals8 = acc8.Or(next.ShiftAllLeft(uint64(bits)))
			acc8 = next.ShiftAllRight(uint64(int(nbits) - bits))
			bits += 32
		} else {
			acc8 = acc8.ShiftAllRight(uint64(nbits))
		}
		bits -= int(nbits)
		vals8.And(mask8).Store(output[op : op+8])
	}
	return n
}
