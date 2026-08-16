//go:build goexperiment.simd && amd64

package pforenc

import (
	"math/bits"
	"simd/archsimd"
)

// hasScanISA covers all requirements for scanSIMD
// and is fulfilled by CPUs like Intel Ice Lake or AMD Zen 4/Zen 5.
var hasScanISA = archsimd.X86.AVX512GFNI() &&
	archsimd.X86.AVX512VBMI() &&
	archsimd.X86.AVX512BITALG()

var scanShuffle, scanUnits [64]uint8

func init() {
	for g := range 2 {
		for p := range 4 {
			for m := range 8 {
				scanShuffle[g*32+p*8+m] = uint8(4*(8*g+m) + p)
			}
		}
	}
	for i := range scanUnits {
		scanUnits[i] = 1 << (i % 8)
	}
}

// scan computes the OR and AND of vals,
// plus cnt[b] = how many values need more than b bits,
// but processing 16 values at a time!
//
// Worked example, 16 values, of which vals[3] = 100 (bit length 7):
//
//	VPLZCNTD  lzcnt(100) = 25                   32-25 = 7 bits
//	VPSRLVD   0xFFFFFFFF >> 25 = 0x0000007F   ← "smear": bits 0..6 set,
//	          00000000000000000000000001111111  one bit per b that 100 exceeds
//	VPERMB    regroup so each qword holds byte p of 8 values:
//	          an 8x8 bit matrix, one value per row
//	VGF2P8AFFINEQB  transpose it: byte i = which of those 8 values have bit i
//	VPOPCNTB  count the bits in each byte:
//	          byte i = how many of the 8 values exceed bit width i
//	VPADDB    add into the running counts
//
// So cnt[] falls out as a column-wise popcount of the smears:
// vals[3] bumps cnt[0..6] each by one.
func scan(output *stats, vals []uint32) {
	if !hasScanISA {
		scanScalar(output, vals)
		return
	}
	or16 := archsimd.BroadcastUint32x16(0)
	and16 := archsimd.BroadcastUint32x16(^uint32(0))
	ones16 := archsimd.BroadcastUint32x16(^uint32(0))
	shuffle := archsimd.LoadUint8x64Array(&scanShuffle)
	units := archsimd.LoadUint8x64Array(&scanUnits)
	var acc archsimd.Uint8x64
	idx := 0
	for ; idx+16 <= len(vals); idx += 16 {
		v := archsimd.LoadUint32x16(vals[idx : idx+16])
		or16 = or16.Or(v)
		and16 = and16.And(v)
		smear := ones16.ShiftRight(v.LeadingZeros()).ReshapeToUint8s()
		matrices := smear.Permute(shuffle).ReshapeToUint64s()
		acc = acc.Add(units.GaloisFieldAffineTransform(matrices, 0).OnesCount())
	}
	// Horizontal OR/AND: fold 16 → 8 → 4 lanes, then 4 scalars.
	or8 := or16.GetLo().Or(or16.GetHi())
	and8 := and16.GetLo().And(and16.GetHi())
	var or4, and4 [4]uint32
	or8.GetLo().Or(or8.GetHi()).StoreArray(&or4)
	and8.GetLo().And(and8.GetHi()).StoreArray(&and4)
	or := or4[0] | or4[1] | or4[2] | or4[3]
	and := and4[0] & and4[1] & and4[2] & and4[3]
	for _, val := range vals[idx:] {
		or |= val
		and &= val
	}
	output.or = or
	output.and = and
	if or == and {
		return // constant block: encode() never looks at cnt
	}
	// Widen the two groups of byte counts to uint16 lanes (so that
	// 128+128 = 256 fits), fold them into cnt[b] for b=0..31,
	// then widen again to the uint32 lanes of output.cnt.
	sum := acc.GetLo().ExtendToUint16().Add(acc.GetHi().ExtendToUint16())
	sum.GetLo().ExtendToUint32().Store(output.cnt[0:16])
	sum.GetHi().ExtendToUint32().Store(output.cnt[16:32])
	for _, val := range vals[idx:] {
		for b := range bits.Len32(val) {
			output.cnt[b]++
		}
	}
}
