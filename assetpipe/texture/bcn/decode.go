package bcn

import (
	"encoding/binary"
	"fmt"
)

// RGBA8 holds one decoded texel as stored 8-bit codes. The codes carry whatever
// transfer function the encoder applied, so RGBA8 is not linear light.
type RGBA8 struct {
	R, G, B, A uint8
}

// Where the decode rules come from
//
// The rules below follow the published BC1 to BC5 decode rules of Direct3D 11
// and of the Khronos Data Format Specification, which agree. Both write the
// palette as weighted sums of the endpoints over normalized values in the range
// 0 to 1, and both write the last two entries of the six-value BC4 mode as the
// constants 0.0 and 1.0. That is why this decoder interpolates in float64 and
// rounds once at the end.
//
// The rules give the weights, not the rounding. A GPU that truncates instead of
// rounding differs from this decoder by at most one code, and only on an
// interpolated entry. Every test in this package that asserts exact bytes picks
// endpoint pairs whose interpolants are whole numbers, so the tolerance never
// decides a test. TestPaletteRoundingIsExactInVectors proves that.

// roundCode rounds one value in code units to the nearest 8-bit code. Ties go
// away from zero, which matches the rounding of texture.LinearToUnorm8.
func roundCode(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// expand5 holds the 8-bit value of every 5-bit endpoint channel. The two low
// bits repeat the two high bits, so code 31 expands to 255 and code 0 expands to
// 0. Direct3D and Vulkan both specify this bit replication.
var expand5 = func() [32]uint8 {
	var out [32]uint8
	for v := range out {
		out[v] = uint8(v<<3 | v>>2)
	}
	return out
}()

// expand6 holds the 8-bit value of every 6-bit endpoint channel.
var expand6 = func() [64]uint8 {
	var out [64]uint8
	for v := range out {
		out[v] = uint8(v<<2 | v>>4)
	}
	return out
}()

// unpack565 expands one RGB565 endpoint to 8-bit codes.
func unpack565(v uint16) RGBA8 {
	return RGBA8{
		R: expand5[(v>>11)&0x1F],
		G: expand6[(v>>5)&0x3F],
		B: expand5[v&0x1F],
		A: 255,
	}
}

// colorPalette builds the four decode entries of one BC1-layout colour block.
//
// forceFour selects the BC3 rule. A BC1 block picks its mode from the integer
// order of the two endpoints; a BC3 colour block always uses the four-colour
// mode, whatever the order is.
func colorPalette(c0, c1 uint16, forceFour bool) [4]RGBA8 {
	a := unpack565(c0)
	b := unpack565(c1)
	var pal [4]RGBA8
	pal[0] = a
	pal[1] = b
	if forceFour || c0 > c1 {
		// Four-colour mode: two interpolants at one third and two thirds.
		pal[2] = RGBA8{
			R: roundCode((2*float64(a.R) + float64(b.R)) / 3),
			G: roundCode((2*float64(a.G) + float64(b.G)) / 3),
			B: roundCode((2*float64(a.B) + float64(b.B)) / 3),
			A: 255,
		}
		pal[3] = RGBA8{
			R: roundCode((float64(a.R) + 2*float64(b.R)) / 3),
			G: roundCode((float64(a.G) + 2*float64(b.G)) / 3),
			B: roundCode((float64(a.B) + 2*float64(b.B)) / 3),
			A: 255,
		}
		return pal
	}
	// Three-colour mode: one midpoint, then transparent black.
	pal[2] = RGBA8{
		R: roundCode((float64(a.R) + float64(b.R)) / 2),
		G: roundCode((float64(a.G) + float64(b.G)) / 2),
		B: roundCode((float64(a.B) + float64(b.B)) / 2),
		A: 255,
	}
	pal[3] = RGBA8{}
	return pal
}

// decodeColorBlock decodes the eight bytes of a BC1-layout colour block.
//
// Layout: color0 as a little-endian uint16, color1 as a little-endian uint16,
// then a little-endian uint32 that holds sixteen 2-bit indices. Texel i sits at
// bits 2i and 2i+1, and the texels run row-major inside the 4x4 block.
func decodeColorBlock(block []byte, forceFour bool) [16]RGBA8 {
	c0 := binary.LittleEndian.Uint16(block[0:2])
	c1 := binary.LittleEndian.Uint16(block[2:4])
	bits := binary.LittleEndian.Uint32(block[4:8])
	pal := colorPalette(c0, c1, forceFour)
	var out [16]RGBA8
	for i := range out {
		out[i] = pal[(bits>>(2*i))&3]
	}
	return out
}

// bc4Palette builds the eight decode entries of one BC4-layout block in code
// units from 0 to 255.
//
// The mode comes from the integer order of the two endpoints. The eight-value
// mode spreads eight levels across the whole range. The six-value mode spends
// two of its entries on the constants 0.0 and 1.0, which pays off on a block
// that holds a hard cutout or a saturated channel.
func bc4Palette(e0, e1 uint8) [8]float64 {
	var pal [8]float64
	a := float64(e0)
	b := float64(e1)
	pal[0] = a
	pal[1] = b
	if e0 > e1 {
		for i := 2; i < 8; i++ {
			pal[i] = (float64(8-i)*a + float64(i-1)*b) / 7
		}
		return pal
	}
	for i := 2; i < 6; i++ {
		pal[i] = (float64(6-i)*a + float64(i-1)*b) / 5
	}
	pal[6] = 0
	pal[7] = 255
	return pal
}

// decodeBC4Codes decodes one BC4-layout block to sixteen 8-bit codes.
//
// Layout: endpoint0, endpoint1, then a little-endian 48-bit field that holds
// sixteen 3-bit indices. Texel i sits at bits 3i to 3i+2.
func decodeBC4Codes(block []byte) [16]uint8 {
	pal := bc4Palette(block[0], block[1])
	bits := uint64(block[2]) | uint64(block[3])<<8 | uint64(block[4])<<16 |
		uint64(block[5])<<24 | uint64(block[6])<<32 | uint64(block[7])<<40
	var out [16]uint8
	for i := range out {
		out[i] = roundCode(pal[(bits>>(3*i))&7])
	}
	return out
}

// DecodeBlockBC1 decodes one 8-byte BC1 block to sixteen texels in row-major
// order. The mode follows the BC1 rule, so an endpoint order of color0 <= color1
// selects the three-colour mode and index 3 decodes to transparent black.
func DecodeBlockBC1(block []byte) ([16]RGBA8, error) {
	if len(block) < 8 {
		return [16]RGBA8{}, fmt.Errorf("%w: BC1 block needs 8 bytes, got %d", ErrPayload, len(block))
	}
	return decodeColorBlock(block, false), nil
}

// DecodeBlockBC3 decodes one 16-byte BC3 block to sixteen texels in row-major
// order. Bytes 0 to 7 hold the alpha block and bytes 8 to 15 hold the colour
// block, which always uses the four-colour mode.
func DecodeBlockBC3(block []byte) ([16]RGBA8, error) {
	if len(block) < 16 {
		return [16]RGBA8{}, fmt.Errorf("%w: BC3 block needs 16 bytes, got %d", ErrPayload, len(block))
	}
	alpha := decodeBC4Codes(block[0:8])
	out := decodeColorBlock(block[8:16], true)
	for i := range out {
		out[i].A = alpha[i]
	}
	return out, nil
}

// DecodeBlockBC4 decodes one 8-byte BC4 block to sixteen normalized values in
// the range 0 to 1. The values are the exact numbers the published rules give,
// with no rounding, so a test can check the rounding on its own.
func DecodeBlockBC4(block []byte) ([16]float64, error) {
	if len(block) < 8 {
		return [16]float64{}, fmt.Errorf("%w: BC4 block needs 8 bytes, got %d", ErrPayload, len(block))
	}
	pal := bc4Palette(block[0], block[1])
	bits := uint64(block[2]) | uint64(block[3])<<8 | uint64(block[4])<<16 |
		uint64(block[5])<<24 | uint64(block[6])<<32 | uint64(block[7])<<40
	var out [16]float64
	for i := range out {
		out[i] = pal[(bits>>(3*i))&7] / 255
	}
	return out, nil
}

// DecodeBlockBC4Codes decodes one BC4 block to sixteen 8-bit codes. It rounds
// the normalized values of DecodeBlockBC4 once, to nearest.
func DecodeBlockBC4Codes(block []byte) ([16]uint8, error) {
	if len(block) < 8 {
		return [16]uint8{}, fmt.Errorf("%w: BC4 block needs 8 bytes, got %d", ErrPayload, len(block))
	}
	return decodeBC4Codes(block), nil
}

// DecodeBlockBC5 decodes one 16-byte BC5 block to sixteen pairs of normalized
// values. Bytes 0 to 7 hold the first channel and bytes 8 to 15 hold the second.
func DecodeBlockBC5(block []byte) ([16][2]float64, error) {
	if len(block) < 16 {
		return [16][2]float64{}, fmt.Errorf("%w: BC5 block needs 16 bytes, got %d", ErrPayload, len(block))
	}
	first, _ := DecodeBlockBC4(block[0:8])
	second, _ := DecodeBlockBC4(block[8:16])
	var out [16][2]float64
	for i := range out {
		out[i] = [2]float64{first[i], second[i]}
	}
	return out, nil
}

// Decode expands one whole mip level back to stored 8-bit codes, four per texel
// in row-major order.
//
// The result holds the same codes the encoder stored, so a caller compares it
// against ReferenceCodes directly. Channels the format does not store read back
// as zero, except alpha, which reads back as 255:
//
//   - FormatBC1RGB and FormatBC1RGBA fill red, green and blue. BC1RGB forces
//     alpha to 255, which is what the RGB VkFormat gives.
//   - FormatBC3 fills all four channels.
//   - FormatBC4 fills red only.
//   - FormatBC5 fills red and green only.
func Decode(f Format, data []byte, width, height int) ([]byte, error) {
	block := f.BlockBytes()
	if block == 0 {
		return nil, fmt.Errorf("%w: %d", ErrFormat, int(f))
	}
	if width < 1 || height < 1 {
		return nil, fmt.Errorf("%w: %dx%d", ErrShape, width, height)
	}
	want := PayloadSize(f, width, height)
	if len(data) != want {
		return nil, fmt.Errorf("%w: %dx%d as %s needs %d bytes, got %d",
			ErrPayload, width, height, f, want, len(data))
	}
	across := BlocksAcross(width)
	down := BlocksAcross(height)
	out := make([]byte, width*height*4)
	for by := 0; by < down; by++ {
		for bx := 0; bx < across; bx++ {
			src := data[(by*across+bx)*block:]
			texels, err := decodeOneBlock(f, src)
			if err != nil {
				return nil, err
			}
			for row := 0; row < 4; row++ {
				y := by*4 + row
				if y >= height {
					break
				}
				for col := 0; col < 4; col++ {
					x := bx*4 + col
					if x >= width {
						break
					}
					t := texels[row*4+col]
					i := (y*width + x) * 4
					out[i] = t.R
					out[i+1] = t.G
					out[i+2] = t.B
					out[i+3] = t.A
				}
			}
		}
	}
	return out, nil
}

// decodeOneBlock routes one block to the decoder of its format.
func decodeOneBlock(f Format, src []byte) ([16]RGBA8, error) {
	switch f {
	case FormatBC1RGB:
		out, err := DecodeBlockBC1(src)
		if err != nil {
			return out, err
		}
		// The RGB VkFormat ignores the alpha bit of the three-colour mode.
		for i := range out {
			out[i].A = 255
		}
		return out, nil
	case FormatBC1RGBA:
		return DecodeBlockBC1(src)
	case FormatBC3:
		return DecodeBlockBC3(src)
	case FormatBC4:
		codes, err := DecodeBlockBC4Codes(src)
		if err != nil {
			return [16]RGBA8{}, err
		}
		var out [16]RGBA8
		for i, c := range codes {
			out[i] = RGBA8{R: c, A: 255}
		}
		return out, nil
	case FormatBC5:
		if len(src) < 16 {
			return [16]RGBA8{}, fmt.Errorf("%w: BC5 block needs 16 bytes, got %d", ErrPayload, len(src))
		}
		red := decodeBC4Codes(src[0:8])
		green := decodeBC4Codes(src[8:16])
		var out [16]RGBA8
		for i := range out {
			out[i] = RGBA8{R: red[i], G: green[i], A: 255}
		}
		return out, nil
	}
	return [16]RGBA8{}, fmt.Errorf("%w: %d", ErrFormat, int(f))
}
