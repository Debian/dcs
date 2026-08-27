//go:build !goexperiment.simd || !amd64

package turbopfor

const HasAVX2 = false

const HasAVX512 = false
