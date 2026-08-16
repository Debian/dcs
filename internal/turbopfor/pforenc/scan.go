package pforenc

import "math/bits"

type stats struct {
	or, and uint32
	hist    [33]uint32 // hist[n] = how many values where bits.Len32(val)==n
}

func scan(output *stats, vals []uint32) {
	or := uint32(0)
	and := ^uint32(0)
	for _, val := range vals {
		or |= val
		and &= val
		output.hist[bits.Len32(val)]++
	}
	output.or = or
	output.and = and
}
