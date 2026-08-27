//go:build goexperiment.simd && amd64 && amd64.v3 && !amd64.v4

package turbopfor

import "simd/archsimd"

const HasAVX2 = true

var HasAVX512 = archsimd.X86.AVX512()
