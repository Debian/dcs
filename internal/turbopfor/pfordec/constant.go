package pfordec

func fillConstantScalar(output []uint32, val uint32) {
	for i := range output {
		output[i] = val
	}
}
