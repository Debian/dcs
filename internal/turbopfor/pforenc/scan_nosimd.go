//go:build !goexperiment.simd || !amd64

package pforenc

func scan(output *stats, vals []uint32) {
	scanScalar(output, vals)
}
