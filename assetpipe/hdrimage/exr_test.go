package hdrimage

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"math"
	"testing"
)

// writeEXR builds a minimal single-part scanline OpenEXR file. It writes half
// channels named B, G, and R, in the alphabetical order the format requires.
func writeEXR(t *testing.T, width, height int, compression int, pixel func(x, y int) (float64, float64, float64)) []byte {
	t.Helper()
	var head bytes.Buffer
	binary.Write(&head, binary.LittleEndian, uint32(20000630))
	binary.Write(&head, binary.LittleEndian, uint32(2))

	writeAttr := func(name, kind string, payload []byte) {
		head.WriteString(name)
		head.WriteByte(0)
		head.WriteString(kind)
		head.WriteByte(0)
		binary.Write(&head, binary.LittleEndian, uint32(len(payload)))
		head.Write(payload)
	}

	var channels bytes.Buffer
	for _, name := range []string{"B", "G", "R"} {
		channels.WriteString(name)
		channels.WriteByte(0)
		binary.Write(&channels, binary.LittleEndian, uint32(exrPixelHalf))
		binary.Write(&channels, binary.LittleEndian, uint32(0)) // pLinear and reserved
		binary.Write(&channels, binary.LittleEndian, uint32(1)) // xSampling
		binary.Write(&channels, binary.LittleEndian, uint32(1)) // ySampling
	}
	channels.WriteByte(0)
	writeAttr("channels", "chlist", channels.Bytes())
	writeAttr("compression", "compression", []byte{byte(compression)})

	var window bytes.Buffer
	binary.Write(&window, binary.LittleEndian, int32(0))
	binary.Write(&window, binary.LittleEndian, int32(0))
	binary.Write(&window, binary.LittleEndian, int32(width-1))
	binary.Write(&window, binary.LittleEndian, int32(height-1))
	writeAttr("dataWindow", "box2i", window.Bytes())
	writeAttr("displayWindow", "box2i", window.Bytes())
	writeAttr("lineOrder", "lineOrder", []byte{0})
	writeAttr("pixelAspectRatio", "float", float32Bytes(1))
	writeAttr("screenWindowCenter", "v2f", append(float32Bytes(0), float32Bytes(0)...))
	writeAttr("screenWindowWidth", "float", float32Bytes(1))
	head.WriteByte(0)

	linesPerBlock := 1
	if compression == exrCompressZIP {
		linesPerBlock = 16
	}
	blocks := (height + linesPerBlock - 1) / linesPerBlock

	var body bytes.Buffer
	offsets := make([]uint64, blocks)
	base := uint64(head.Len() + blocks*8)
	for block := 0; block < blocks; block++ {
		startY := block * linesPerBlock
		lines := linesPerBlock
		if startY+lines > height {
			lines = height - startY
		}
		var raw bytes.Buffer
		for line := 0; line < lines; line++ {
			y := startY + line
			for _, channel := range []string{"B", "G", "R"} {
				for x := 0; x < width; x++ {
					r, g, b := pixel(x, y)
					value := r
					switch channel {
					case "G":
						value = g
					case "B":
						value = b
					}
					binary.Write(&raw, binary.LittleEndian, Float32ToHalf(float32(value)))
				}
			}
		}
		payload := raw.Bytes()
		if compression == exrCompressZIP || compression == exrCompressZIPS {
			payload = exrCompressBlock(payload)
			if len(payload) >= raw.Len() {
				payload = raw.Bytes()
			}
		}
		offsets[block] = base + uint64(body.Len())
		binary.Write(&body, binary.LittleEndian, int32(startY))
		binary.Write(&body, binary.LittleEndian, int32(len(payload)))
		body.Write(payload)
	}

	var out bytes.Buffer
	out.Write(head.Bytes())
	for _, offset := range offsets {
		binary.Write(&out, binary.LittleEndian, offset)
	}
	out.Write(body.Bytes())
	return out.Bytes()
}

// exrCompressBlock applies the interleave and delta steps OpenEXR uses before
// DEFLATE. It is the inverse of exrReconstruct.
func exrCompressBlock(raw []byte) []byte {
	half := (len(raw) + 1) / 2
	shuffled := make([]byte, len(raw))
	first, second := 0, half
	for i := 0; i < len(raw); i++ {
		if i%2 == 0 {
			shuffled[first] = raw[i]
			first++
			continue
		}
		shuffled[second] = raw[i]
		second++
	}
	for i := len(shuffled) - 1; i > 0; i-- {
		delta := int(shuffled[i]) - int(shuffled[i-1]) + (128 + 256)
		shuffled[i] = byte(delta)
	}
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	zw.Write(shuffled)
	zw.Close()
	return buf.Bytes()
}

func float32Bytes(v float32) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, math.Float32bits(v))
	return out
}

func exrPixel(x, y int) (float64, float64, float64) {
	return float64(x) * 0.25, float64(y) * 0.5, 2
}

func TestDecodeEXRUncompressed(t *testing.T) {
	data := writeEXR(t, 8, 4, exrCompressNone, exrPixel)
	img, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img.Width != 8 || img.Height != 4 || img.Source != "exr" {
		t.Fatalf("unexpected image %dx%d source %q", img.Width, img.Height, img.Source)
	}
	checkEXRPixels(t, img)
}

func TestDecodeEXRZIP(t *testing.T) {
	data := writeEXR(t, 32, 20, exrCompressZIP, exrPixel)
	img, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img.Width != 32 || img.Height != 20 {
		t.Fatalf("unexpected image %dx%d", img.Width, img.Height)
	}
	checkEXRPixels(t, img)
}

func TestDecodeEXRZIPS(t *testing.T) {
	data := writeEXR(t, 16, 5, exrCompressZIPS, exrPixel)
	img, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	checkEXRPixels(t, img)
}

func TestDecodeEXRRejectsLossyCompression(t *testing.T) {
	data := writeEXR(t, 8, 4, exrCompressNone, exrPixel)
	// Patch the compression attribute to PIZ.
	idx := bytes.Index(data, []byte("compression\x00compression\x00"))
	if idx < 0 {
		t.Fatal("compression attribute not found")
	}
	data[idx+len("compression\x00compression\x00")+4] = exrCompressPIZ
	if _, err := Decode(data); err == nil {
		t.Fatal("expected an unsupported compression error")
	}
}

func checkEXRPixels(t *testing.T, img *Image) {
	t.Helper()
	for y := 0; y < img.Height; y++ {
		for x := 0; x < img.Width; x++ {
			wantR, wantG, wantB := exrPixel(x, y)
			r, g, b := img.At(x, y)
			if math.Abs(float64(r)-wantR) > 1e-3 || math.Abs(float64(g)-wantG) > 1e-3 || math.Abs(float64(b)-wantB) > 1e-3 {
				t.Fatalf("pixel (%d,%d) = (%v,%v,%v), want (%v,%v,%v)", x, y, r, g, b, wantR, wantG, wantB)
			}
		}
	}
}
