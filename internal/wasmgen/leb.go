package wasmgen

// AppendU32 appends the shortest unsigned LEB128 representation of value.
func AppendU32(dst []byte, value uint32) []byte {
	for value >= 0x80 {
		dst = append(dst, byte(value)|0x80)
		value >>= 7
	}
	return append(dst, byte(value))
}

// AppendI32 appends the shortest signed LEB128 representation of value.
func AppendI32(dst []byte, value int32) []byte {
	return AppendI64(dst, int64(value))
}

// AppendI64 appends the shortest signed LEB128 representation of value.
func AppendI64(dst []byte, value int64) []byte {
	for {
		b := byte(value) & 0x7f
		value >>= 7
		last := value == 0 && b&0x40 == 0 || value == -1 && b&0x40 != 0
		if !last {
			b |= 0x80
		}
		dst = append(dst, b)
		if last {
			return dst
		}
	}
}
