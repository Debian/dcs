//go:build !goexperiment.simd || !amd64

package pforenc

func bitpack256v(dest []byte, vals []uint32, bitWidth int) []byte {
	return bitpack256vScalar(dest, vals, bitWidth)
}
