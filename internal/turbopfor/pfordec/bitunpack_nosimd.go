//go:build !goexperiment.simd || !amd64

package pfordec

func bitunpack256v32(fullinput []byte, fulloutput []uint32, nbits int) (read int) {
	return bitunpack256v32Scalar(fullinput, fulloutput, nbits)
}
