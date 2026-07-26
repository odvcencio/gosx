package ktx2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func TestEncodeRoundTripUncompressed(t *testing.T) {
	level0 := make([]byte, 4*4*4)
	for i := range level0 {
		level0[i] = byte(i)
	}
	level1 := make([]byte, 2*2*4)
	for i := range level1 {
		level1[i] = byte(255 - i)
	}
	level2 := make([]byte, 1*1*4)
	src := &Image{
		Format: VkFormatR8G8B8A8Unorm,
		Width:  4,
		Height: 4,
		Faces:  1,
		Levels: []Level{{Bytes: level0}, {Bytes: level1}, {Bytes: level2}},
	}
	data, err := Encode(src, EncodeOptions{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !bytes.Equal(data[:12], identifier) {
		t.Fatalf("identifier mismatch")
	}
	img, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if img.Format != VkFormatR8G8B8A8Unorm || img.Width != 4 || img.Height != 4 || len(img.Levels) != 3 {
		t.Fatalf("unexpected image: %+v", img)
	}
	if !bytes.Equal(img.Levels[0].Bytes, level0) || !bytes.Equal(img.Levels[1].Bytes, level1) {
		t.Fatalf("level payload mismatch")
	}
	// The specification stores the smallest mip first in the file.
	off0 := binary.LittleEndian.Uint64(data[80:])
	off2 := binary.LittleEndian.Uint64(data[80+2*24:])
	if off2 >= off0 {
		t.Fatalf("expected the smallest level first: level0 at %d, level2 at %d", off0, off2)
	}
}

func TestEncodeCubemapZlib(t *testing.T) {
	size := 2
	payload := make([]byte, size*size*8*6)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	src := &Image{
		Format: VkFormatR16G16B16A16Sfloat,
		Width:  size,
		Height: size,
		Faces:  6,
		Levels: []Level{{Bytes: payload}},
	}
	data, err := Encode(src, EncodeOptions{Supercompression: SupercompressionZlib})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	img, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if img.Faces != 6 {
		t.Fatalf("faces = %d, want 6", img.Faces)
	}
	if !bytes.Equal(img.Levels[0].Bytes, payload) {
		t.Fatalf("zlib round trip mismatch")
	}
}

func TestEncodeRejectsBadShape(t *testing.T) {
	src := &Image{
		Format: VkFormatR8G8B8A8Unorm,
		Width:  4,
		Height: 4,
		Faces:  1,
		Levels: []Level{{Bytes: make([]byte, 3)}},
	}
	if _, err := Encode(src, EncodeOptions{}); !errors.Is(err, ErrEncodeShape) {
		t.Fatalf("err = %v, want ErrEncodeShape", err)
	}
}

func TestEncodeRejectsBlockCompressedFormat(t *testing.T) {
	src := &Image{
		Format: VkFormatBC7UnormBlock,
		Width:  4,
		Height: 4,
		Faces:  1,
		Levels: []Level{{Bytes: make([]byte, 16)}},
	}
	if _, err := Encode(src, EncodeOptions{}); !errors.Is(err, ErrEncodeFormat) {
		t.Fatalf("err = %v, want ErrEncodeFormat", err)
	}
}

func TestEncodeWritesBasicDescriptorAndKeyValues(t *testing.T) {
	src := &Image{
		Format: VkFormatR16G16Sfloat,
		Width:  2,
		Height: 2,
		Faces:  1,
		Levels: []Level{{Bytes: make([]byte, 2*2*4)}},
	}
	data, err := Encode(src, EncodeOptions{Writer: "unit test", KeyValues: map[string]string{"GoSXtest": "value"}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	h := parseHeader(data[12:80])
	if h.typeSize != 2 {
		t.Fatalf("typeSize = %d, want 2", h.typeSize)
	}
	if h.dfdByteOffset%4 != 0 || h.kvdByteOffset%4 != 0 {
		t.Fatalf("descriptor blocks must stay 4-byte aligned: dfd=%d kvd=%d", h.dfdByteOffset, h.kvdByteOffset)
	}
	dfd := data[h.dfdByteOffset : h.dfdByteOffset+h.dfdByteLength]
	if got := binary.LittleEndian.Uint32(dfd); got != h.dfdByteLength {
		t.Fatalf("dfd total size = %d, want %d", got, h.dfdByteLength)
	}
	if got := binary.LittleEndian.Uint32(dfd[4:]); got != 0 {
		t.Fatalf("vendorId/descriptorType = %d, want 0", got)
	}
	version := binary.LittleEndian.Uint32(dfd[8:])
	if version&0xFFFF != 2 {
		t.Fatalf("descriptor version = %d, want 2", version&0xFFFF)
	}
	blockSize := version >> 16
	// Two channels, 24 header bytes plus 16 bytes per sample.
	if blockSize != 24+16*2 {
		t.Fatalf("descriptorBlockSize = %d, want %d", blockSize, 24+16*2)
	}
	if got := binary.LittleEndian.Uint32(dfd[20:]); got != 4 {
		t.Fatalf("bytesPlane0 = %d, want 4", got)
	}
	// The first sample must declare the red channel as a signed float.
	sample := binary.LittleEndian.Uint32(dfd[28:])
	if bitLength := (sample >> 16) & 0xFF; bitLength != 15 {
		t.Fatalf("bitLength = %d, want 15 (16 bits minus one)", bitLength)
	}
	channelType := sample >> 24
	if channelType&0x0F != 0 || channelType&0x80 == 0 || channelType&0x40 == 0 {
		t.Fatalf("channelType = %#x, want red with signed and float qualifiers", channelType)
	}
	lower := binary.LittleEndian.Uint32(dfd[36:])
	upper := binary.LittleEndian.Uint32(dfd[40:])
	if math.Float32frombits(lower) != -1 || math.Float32frombits(upper) != 1 {
		t.Fatalf("sample range = [%v, %v], want [-1, 1]", math.Float32frombits(lower), math.Float32frombits(upper))
	}

	kv, err := KeyValues(data)
	if err != nil {
		t.Fatalf("key values: %v", err)
	}
	if kv["KTXwriter"] != "unit test" || kv["GoSXtest"] != "value" {
		t.Fatalf("unexpected key values: %+v", kv)
	}
}

func TestEncodeMipPaddingAlignment(t *testing.T) {
	src := &Image{
		Format: VkFormatR16G16B16A16Sfloat,
		Width:  4,
		Height: 4,
		Faces:  1,
		Levels: []Level{
			{Bytes: make([]byte, 4*4*8)},
			{Bytes: make([]byte, 2*2*8)},
			{Bytes: make([]byte, 1*1*8)},
		},
	}
	data, err := Encode(src, EncodeOptions{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	align := lcm(8, 4)
	for level := 0; level < 3; level++ {
		off := binary.LittleEndian.Uint64(data[80+level*24:])
		if off%uint64(align) != 0 {
			t.Fatalf("level %d offset %d is not aligned to %d", level, off, align)
		}
	}
}
