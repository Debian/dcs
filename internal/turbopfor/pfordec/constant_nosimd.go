//go:build !goexperiment.simd || !amd64

package pfordec

func fillConstant(output []uint32, val uint32) {
	fillConstantScalar(output, val)
}
