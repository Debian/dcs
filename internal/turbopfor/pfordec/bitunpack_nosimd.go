//go:build !goexperiment.simd || !amd64

package pfordec

func bitunpack(input []byte, output []uint32, bitWidth int) (read int) {
	return bitunpackScalar(input, output, bitWidth)
}

func bitunpack256v32(fullinput []byte, fulloutput []uint32, nbits int) (read int) {
	return bitunpack256v32Scalar(fullinput, fulloutput, nbits)
}

func bitunpack256v32Ex(fullinput []byte, fulloutput []uint32, nbits int, exmap *[32]byte, exceptions *[256]uint32) (read int) {
	return bitunpack256v32ExScalar(fullinput, fulloutput, nbits, exmap, exceptions)
}
