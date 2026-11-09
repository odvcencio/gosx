package meshopt

import "fmt"

const (
	vertexBlockBytes = 8192
	vertexBlockMax   = 256
	byteGroup        = 16
	vertexHeader     = 0xa0
)

// vertexBlockSize returns how many vertices one block holds.
func vertexBlockSize(stride int) int {
	n := (vertexBlockBytes / stride) &^ (byteGroup - 1)
	if n > vertexBlockMax {
		n = vertexBlockMax
	}
	return n
}

func checkStride(stride int) error {
	if stride <= 0 || stride > 256 || stride%4 != 0 {
		return fmt.Errorf("meshopt: vertex stride %d must be a multiple of 4 up to 256", stride)
	}
	return nil
}

// EncodeVertexBuffer compresses count vertices of stride bytes each.
//
// Each byte column is delta coded against the same byte of the previous
// vertex, zigzag mapped, and stored 16 values at a time in the smallest of
// four forms: all zero, 2 bits, 4 bits, or 8 bits per value. A packed value at
// the form's maximum escapes to a full byte after the packed bits.
func EncodeVertexBuffer(data []byte, count, stride int) ([]byte, error) {
	if err := checkStride(stride); err != nil {
		return nil, err
	}
	if len(data) != count*stride {
		return nil, fmt.Errorf("meshopt: %d bytes is not %d vertices of %d bytes", len(data), count, stride)
	}
	first := make([]byte, stride)
	if count > 0 {
		copy(first, data[:stride])
	}
	last := append([]byte(nil), first...)
	out := []byte{vertexHeader}
	block := vertexBlockSize(stride)
	deltas := make([]byte, vertexBlockMax)
	for start := 0; start < count; start += block {
		n := min(block, count-start)
		aligned := (n + byteGroup - 1) &^ (byteGroup - 1)
		groups := aligned / byteGroup
		for k := 0; k < stride; k++ {
			for i := range deltas[:aligned] {
				deltas[i] = 0
			}
			prev := last[k]
			for v := 0; v < n; v++ {
				cur := data[(start+v)*stride+k]
				d := int8(cur - prev)
				deltas[v] = byte((d << 1) ^ (d >> 7))
				prev = cur
			}
			last[k] = prev
			header := make([]byte, (groups+3)/4)
			var body []byte
			for g := 0; g < groups; g++ {
				mode, enc := encodeGroup(deltas[g*byteGroup : (g+1)*byteGroup])
				header[g>>2] |= byte(mode) << ((g & 3) * 2)
				body = append(body, enc...)
			}
			out = append(out, header...)
			out = append(out, body...)
		}
	}
	// The tail holds the first vertex in its last stride bytes, padded with
	// zeros in front to at least 32 bytes, as the reference decoder requires.
	return append(append(out, make([]byte, vertexTail(stride)-stride)...), first...), nil
}

func vertexTail(stride int) int {
	return max(stride, 32)
}

// encodeGroup picks the smallest form for 16 zigzag deltas.
func encodeGroup(v []byte) (int, []byte) {
	var esc2, esc4 int
	for _, x := range v {
		if x >= 3 {
			esc2++
		}
		if x >= 15 {
			esc4++
		}
	}
	if esc2 == 0 && esc4 == 0 {
		zero := true
		for _, x := range v {
			zero = zero && x == 0
		}
		if zero {
			return 0, nil
		}
	}
	size1, size2 := 4+esc2, 8+esc4
	switch {
	case size1 <= size2 && size1 < byteGroup:
		return 1, packGroup(v, 2)
	case size2 < byteGroup:
		return 2, packGroup(v, 4)
	}
	return 3, append([]byte(nil), v...)
}

func packGroup(v []byte, bits int) []byte {
	sentinel := byte(1<<bits - 1)
	out := make([]byte, byteGroup*bits/8)
	var escapes []byte
	for i, x := range v {
		stored := x
		if x >= sentinel {
			stored = sentinel
			escapes = append(escapes, x)
		}
		out[(i*bits)>>3] |= stored << (8 - bits - ((i * bits) & 7))
	}
	return append(out, escapes...)
}

// DecodeVertexBuffer reverses EncodeVertexBuffer. It exists to verify output.
func DecodeVertexBuffer(src []byte, count, stride int) ([]byte, error) {
	if err := checkStride(stride); err != nil {
		return nil, err
	}
	if len(src) < 1+vertexTail(stride) || src[0] != vertexHeader {
		return nil, fmt.Errorf("meshopt: bad vertex buffer header")
	}
	out := make([]byte, count*stride)
	last := append([]byte(nil), src[len(src)-stride:]...)
	end := len(src) - vertexTail(stride)
	pos := 1
	block := vertexBlockSize(stride)
	column := make([]byte, vertexBlockMax)
	for start := 0; start < count; start += block {
		n := min(block, count-start)
		aligned := (n + byteGroup - 1) &^ (byteGroup - 1)
		groups := aligned / byteGroup
		headerSize := (groups + 3) / 4
		for k := 0; k < stride; k++ {
			if pos+headerSize > end {
				return nil, fmt.Errorf("meshopt: truncated vertex data")
			}
			header := pos
			pos += headerSize
			for g := 0; g < groups; g++ {
				mode := int(src[header+(g>>2)]>>((g&3)*2)) & 3
				var err error
				if pos, err = decodeGroup(src[:end], pos, mode, column[g*byteGroup:(g+1)*byteGroup]); err != nil {
					return nil, err
				}
			}
			prev := last[k]
			for v := 0; v < n; v++ {
				z := column[v]
				prev += (z >> 1) ^ -(z & 1)
				out[(start+v)*stride+k] = prev
			}
			last[k] = prev
		}
	}
	return out, nil
}

func decodeGroup(src []byte, pos, mode int, out []byte) (int, error) {
	switch mode {
	case 0:
		for i := range out {
			out[i] = 0
		}
		return pos, nil
	case 3:
		if pos+byteGroup > len(src) {
			return 0, fmt.Errorf("meshopt: truncated vertex data")
		}
		copy(out, src[pos:pos+byteGroup])
		return pos + byteGroup, nil
	}
	bits := 2
	if mode == 2 {
		bits = 4
	}
	sentinel := byte(1<<bits - 1)
	escape := pos + byteGroup*bits/8
	if escape > len(src) {
		return 0, fmt.Errorf("meshopt: truncated vertex data")
	}
	for i := range out {
		x := (src[pos+((i*bits)>>3)] >> (8 - bits - ((i * bits) & 7))) & sentinel
		if x == sentinel {
			if escape >= len(src) {
				return 0, fmt.Errorf("meshopt: truncated vertex data")
			}
			x = src[escape]
			escape++
		}
		out[i] = x
	}
	return escape, nil
}
