package pforenc

func scan(vals []uint32) (or, and uint32) {
	or = 0
	and = ^uint32(0)
	for _, val := range vals {
		or |= val
		and &= val
	}
	return or, and
}
