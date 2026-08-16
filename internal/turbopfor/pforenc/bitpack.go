package pforenc

import "encoding/binary"

func exbitmapScalar(vals []uint32, bitWidth int, exmap []byte, high []uint32) int {
	for idx, val := range vals {
		if rest := val >> bitWidth; rest != 0 {
			exmap[idx/8] |= 1 << (idx % 8) // set bit in the exception bitmap
			high = append(high, rest)      // store high bits
		}
	}
	return len(high)
}

func bitpack(dest []byte, vals []uint32, bitWidth int) []byte {
	mask := uint32(1<<bitWidth - 1)
	var acc uint64
	var have int
	for _, val := range vals {
		acc |= uint64(val&mask) << have
		have += bitWidth
		for have >= 32 {
			dest = binary.LittleEndian.AppendUint32(dest, uint32(acc))
			acc >>= 32
			have -= 32
		}
	}
	for have > 0 {
		dest = append(dest, byte(acc))
		acc >>= 8
		have -= 8
	}
	return dest
}

func bitpack256vScalar(dest []byte, vals []uint32, bitWidth int) []byte {
	mask := uint32(1<<bitWidth - 1)
	// NOTE: These [8]uint64 live on the stack, not in registers.
	// SIMD implementations like bitpack256vSIMD use registers.
	var acc [8]uint64
	var have int
	for idx := 0; idx < len(vals); idx += 8 {
		acc[0] |= uint64(vals[idx]&mask) << have
		acc[1] |= uint64(vals[idx+1]&mask) << have
		acc[2] |= uint64(vals[idx+2]&mask) << have
		acc[3] |= uint64(vals[idx+3]&mask) << have
		acc[4] |= uint64(vals[idx+4]&mask) << have
		acc[5] |= uint64(vals[idx+5]&mask) << have
		acc[6] |= uint64(vals[idx+6]&mask) << have
		acc[7] |= uint64(vals[idx+7]&mask) << have
		have += bitWidth
		for have >= 32 {
			dest = binary.LittleEndian.AppendUint32(dest, uint32(acc[0]))
			acc[0] >>= 32
			dest = binary.LittleEndian.AppendUint32(dest, uint32(acc[1]))
			acc[1] >>= 32
			dest = binary.LittleEndian.AppendUint32(dest, uint32(acc[2]))
			acc[2] >>= 32
			dest = binary.LittleEndian.AppendUint32(dest, uint32(acc[3]))
			acc[3] >>= 32
			dest = binary.LittleEndian.AppendUint32(dest, uint32(acc[4]))
			acc[4] >>= 32
			dest = binary.LittleEndian.AppendUint32(dest, uint32(acc[5]))
			acc[5] >>= 32
			dest = binary.LittleEndian.AppendUint32(dest, uint32(acc[6]))
			acc[6] >>= 32
			dest = binary.LittleEndian.AppendUint32(dest, uint32(acc[7]))
			acc[7] >>= 32
			have -= 32
		}
	}
	return dest

}
