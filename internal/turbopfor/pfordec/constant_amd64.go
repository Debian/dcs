//go:build goexperiment.simd && amd64

package pfordec

import (
	"simd/archsimd"

	"github.com/Debian/dcs/internal/turbopfor"
)

func fillConstant(output []uint32, val uint32) {
	if !turbopfor.HasAVX2 {
		fillConstantScalar(output, val)
		return
	}
	val8 := archsimd.BroadcastUint32x8(val)
	i := 0
	for ; i+8 <= len(output); i += 8 {
		val8.StoreArray((*[8]uint32)(output[i : i+8]))
	}
	fillConstantScalar(output[i:], val)
}
