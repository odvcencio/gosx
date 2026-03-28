package encoding

import "fmt"

// AppendULEB128 appends an unsigned LEB128-encoded integer to dst.
func AppendULEB128(dst []byte, value uint64) []byte {
	for {
		b := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			dst = append(dst, b|0x80)
			continue
		}
		dst = append(dst, b)
		return dst
	}
}

// ReadULEB128 decodes an unsigned LEB128 integer from src.
func ReadULEB128(src []byte) (uint64, int, error) {
	var (
		value uint64
		shift uint
	)
	for i, b := range src {
		value |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, i + 1, nil
		}
		shift += 7
		if shift >= 64 {
			return 0, 0, fmt.Errorf("uleb128 overflows uint64")
		}
	}
	return 0, 0, fmt.Errorf("truncated uleb128 value")
}
