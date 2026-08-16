package pforenc

func vbenc32(dst []byte, val uint32) []byte {
	switch {
	case val < 177:
		return append(dst,
			byte(val))

	case val < 16561:
		val -= 177
		return append(dst,
			byte(177+val>>8),
			byte(val))

	case val < 540849:
		val -= 16561
		return append(dst,
			byte(241+val>>16),
			byte(val),
			byte(val>>8))

	case val < 1<<24:
		return append(dst,
			249,
			byte(val),
			byte(val>>8),
			byte(val>>16))

	default:
		return append(dst,
			250,
			byte(val),
			byte(val>>8),
			byte(val>>16),
			byte(val>>24))
	}
}
