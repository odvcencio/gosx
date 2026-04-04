package quant

// packedSize returns the number of bytes needed to store count b-bit indices.
func packedSize(count, bitWidth int) int {
	return (count*bitWidth + 7) / 8
}

// packIndices packs count b-bit indices into dst.
func packIndices(dst []byte, indices []int, bitWidth int) {
	for i := range dst {
		dst[i] = 0
	}
	switch bitWidth {
	case 1:
		for i, idx := range indices {
			if idx != 0 {
				dst[i/8] |= 1 << uint(i%8)
			}
		}
	case 2:
		for i, idx := range indices {
			dst[i/4] |= byte(idx&3) << uint((i%4)*2)
		}
	case 4:
		for i, idx := range indices {
			dst[i/2] |= byte(idx&15) << uint((i%2)*4)
		}
	case 8:
		for i, idx := range indices {
			dst[i] = byte(idx)
		}
	default:
		bitPos := 0
		for _, idx := range indices {
			val := idx
			for b := 0; b < bitWidth; b++ {
				if val&1 != 0 {
					dst[bitPos/8] |= 1 << uint(bitPos%8)
				}
				val >>= 1
				bitPos++
			}
		}
	}
}

// unpackIndices unpacks count b-bit indices from src.
func unpackIndices(indices []int, src []byte, count, bitWidth int) {
	switch bitWidth {
	case 1:
		for i := 0; i < count; i++ {
			indices[i] = int((src[i/8] >> uint(i%8)) & 1)
		}
	case 2:
		for i := 0; i < count; i++ {
			indices[i] = int((src[i/4] >> uint((i%4)*2)) & 3)
		}
	case 4:
		for i := 0; i < count; i++ {
			indices[i] = int((src[i/2] >> uint((i%2)*4)) & 15)
		}
	case 8:
		for i := 0; i < count; i++ {
			indices[i] = int(src[i])
		}
	default:
		bitPos := 0
		mask := (1 << uint(bitWidth)) - 1
		for i := 0; i < count; i++ {
			val := 0
			for b := 0; b < bitWidth; b++ {
				if src[bitPos/8]&(1<<uint(bitPos%8)) != 0 {
					val |= 1 << uint(b)
				}
				bitPos++
			}
			indices[i] = val & mask
		}
	}
}

// packSigns packs boolean signs (1 bit each) into dst.
func packSigns(dst []byte, signs []bool) {
	for i := range dst {
		dst[i] = 0
	}
	for i, s := range signs {
		if s {
			dst[i/8] |= 1 << uint(i%8)
		}
	}
}

// unpackSigns unpacks sign bits from src.
func unpackSigns(signs []bool, src []byte, count int) {
	for i := 0; i < count; i++ {
		signs[i] = src[i/8]&(1<<uint(i%8)) != 0
	}
}
