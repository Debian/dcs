//go:build goexperiment.simd && amd64 && !amd64.v3

package turbopfor

import "simd/archsimd"

// Compiled with GOAMD64=v1 or =v2,
// need to use runtime detection to find supported ISAs

var HasAVX2 = archsimd.X86.AVX2()

var HasAVX512 = archsimd.X86.AVX512()
