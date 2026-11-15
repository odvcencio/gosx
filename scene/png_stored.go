package scene

import "encoding/binary"

// encodeStoredPNG writes an 8-bit RGB PNG whose pixel data is not compressed
// (stored deflate blocks). It exists so js/wasm builds can emit a PNG without
// linking the standard library's image and compress packages. rgb holds
// width*height*3 bytes, row-major.
func encodeStoredPNG(rgb []byte, width, height int) []byte {
	stride := width * 3
	raw := make([]byte, 0, (stride+1)*height)
	for y := 0; y < height; y++ {
		raw = append(raw, 0)
		raw = append(raw, rgb[y*stride:(y+1)*stride]...)
	}

	blocks := max((len(raw)+65534)/65535, 1)
	z := make([]byte, 0, 2+len(raw)+5*blocks+4)
	z = append(z, 0x78, 0x01)
	for off := 0; ; {
		n := min(len(raw)-off, 65535)
		final := byte(0)
		if off+n >= len(raw) {
			final = 1
		}
		z = append(z, final, byte(n), byte(n>>8), byte(^n), byte(^n>>8))
		z = append(z, raw[off:off+n]...)
		off += n
		if final == 1 {
			break
		}
	}
	z = binary.BigEndian.AppendUint32(z, pngAdler32(raw))

	out := make([]byte, 0, 8+(12+13)+(12+len(z))+12)
	out = append(out, "\x89PNG\r\n\x1a\n"...)
	ihdr := binary.BigEndian.AppendUint32(make([]byte, 0, 13), uint32(width))
	ihdr = binary.BigEndian.AppendUint32(ihdr, uint32(height))
	ihdr = append(ihdr, 8, 2, 0, 0, 0)
	out = appendPNGChunk(out, "IHDR", ihdr)
	out = appendPNGChunk(out, "IDAT", z)
	return appendPNGChunk(out, "IEND", nil)
}

func appendPNGChunk(out []byte, kind string, data []byte) []byte {
	out = binary.BigEndian.AppendUint32(out, uint32(len(data)))
	start := len(out)
	out = append(out, kind...)
	out = append(out, data...)
	return binary.BigEndian.AppendUint32(out, pngCRC32(out[start:]))
}

var pngCRCTable = makePNGCRCTable()

func makePNGCRCTable() (t [256]uint32) {
	for n := range t {
		c := uint32(n)
		for i := 0; i < 8; i++ {
			if c&1 == 1 {
				c = c>>1 ^ 0xedb88320
			} else {
				c >>= 1
			}
		}
		t[n] = c
	}
	return t
}

func pngCRC32(b []byte) uint32 {
	crc := ^uint32(0)
	for _, v := range b {
		crc = pngCRCTable[byte(crc)^v] ^ crc>>8
	}
	return ^crc
}

func pngAdler32(b []byte) uint32 {
	const mod = 65521
	a, s := uint32(1), uint32(0)
	for _, v := range b {
		a = (a + uint32(v)) % mod
		s = (s + a) % mod
	}
	return s<<16 | a
}
