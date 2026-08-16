//go:build !goexperiment.simd || !amd64

package pforenc

func bitpack256v(dest []byte, vals []uint32, bitWidth int) []byte {
	return bitpack256vScalar(dest, vals, bitWidth)
}

func exbitmap(vals []uint32, bitWidth int, exmap []byte, high []uint32) int {
	return exbitmapScalar(vals, bitWidth, exmap, high)
}
