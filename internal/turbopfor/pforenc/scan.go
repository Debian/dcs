package pforenc

import "math/bits"

type stats struct {
	or, and uint32
	// cnt[n] = how many values where bits.Len32(val)>n,
	// i.e. how many exceptions are required for bitWidth=n.
	// Padded so that cnt[b+24] is always in bounds.
	cnt [32 + 24]uint32
}

func scan(output *stats, vals []uint32) {
	or := uint32(0)
	and := ^uint32(0)
	var hist [33]uint32 // hist[n] = how many values where bits.Len32(val)==n
	for _, val := range vals {
		or |= val
		and &= val
		hist[bits.Len32(val)]++
	}
	output.or = or
	output.and = and
	if or == and {
		return // constant block, output.cnt is not needed
	}
	// Turn hist (hist[n] = values with bits.Len32(val)==n)
	// into suffix sums: cnt[n] = values with bits.Len32(val)>n.
	cnt := uint32(0)
	for b := bits.Len32(or) - 1; b >= 0; b-- {
		cnt += hist[b+1]
		output.cnt[b] = cnt
	}
}
