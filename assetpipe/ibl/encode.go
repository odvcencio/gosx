package ibl

import (
	"encoding/binary"
	"fmt"

	"m31labs.dev/gosx/assetpipe/hdrimage"
	"m31labs.dev/gosx/render/bundle/ktx2"
)

// EncodeCubeKTX2 writes a cubemap mip chain as an RGBA16F KTX2 container.
// Alpha stays 1. Half float keeps high dynamic range values that an 8-bit
// container would clip.
func EncodeCubeKTX2(chain Chain, keyValues map[string]string) ([]byte, error) {
	return EncodeCubeKTX2Options(chain, keyValues, ktx2.SupercompressionNone)
}

// EncodeCubeKTX2Options writes a cubemap and selects the payload
// supercompression scheme.
func EncodeCubeKTX2Options(chain Chain, keyValues map[string]string, scheme ktx2.Supercompression) ([]byte, error) {
	if len(chain) == 0 {
		return nil, fmt.Errorf("ibl: empty cubemap chain")
	}
	img := &ktx2.Image{
		Format: ktx2.VkFormatR16G16B16A16Sfloat,
		Width:  chain[0].Size,
		Height: chain[0].Size,
		Depth:  0,
		Layers: 0,
		Faces:  6,
		Levels: make([]ktx2.Level, len(chain)),
	}
	for level, cube := range chain {
		size := cube.Size
		bytesPerFace := size * size * 4 * 2
		payload := make([]byte, bytesPerFace*6)
		for face := 0; face < 6; face++ {
			base := face * bytesPerFace
			source := cube.Faces[face]
			for texel := 0; texel < size*size; texel++ {
				off := base + texel*8
				binary.LittleEndian.PutUint16(payload[off+0:], hdrimage.Float32ToHalf(source[texel*3+0]))
				binary.LittleEndian.PutUint16(payload[off+2:], hdrimage.Float32ToHalf(source[texel*3+1]))
				binary.LittleEndian.PutUint16(payload[off+4:], hdrimage.Float32ToHalf(source[texel*3+2]))
				binary.LittleEndian.PutUint16(payload[off+6:], hdrimage.Float32ToHalf(1))
			}
		}
		img.Levels[level] = ktx2.Level{
			Width:  size,
			Height: size,
			Depth:  1,
			Layers: 1,
			Faces:  6,
			Bytes:  payload,
		}
	}
	return ktx2.Encode(img, ktx2.EncodeOptions{
		Writer:           "GoSX assetpipe ibl",
		KeyValues:        keyValues,
		Supercompression: scheme,
		ZlibLevel:        6,
	})
}

// EncodeBRDFLUTKTX2 writes the split-sum lookup table as an RG16F KTX2
// container with one level.
func EncodeBRDFLUTKTX2(lut *BRDFLUT, keyValues map[string]string) ([]byte, error) {
	if lut == nil || lut.Size <= 0 {
		return nil, fmt.Errorf("ibl: empty brdf lookup table")
	}
	payload := make([]byte, lut.Size*lut.Size*4)
	for i := 0; i < lut.Size*lut.Size; i++ {
		binary.LittleEndian.PutUint16(payload[i*4+0:], hdrimage.Float32ToHalf(lut.Data[i*2+0]))
		binary.LittleEndian.PutUint16(payload[i*4+2:], hdrimage.Float32ToHalf(lut.Data[i*2+1]))
	}
	img := &ktx2.Image{
		Format: ktx2.VkFormatR16G16Sfloat,
		Width:  lut.Size,
		Height: lut.Size,
		Faces:  1,
		Levels: []ktx2.Level{{
			Width:  lut.Size,
			Height: lut.Size,
			Depth:  1,
			Layers: 1,
			Faces:  1,
			Bytes:  payload,
		}},
	}
	return ktx2.Encode(img, ktx2.EncodeOptions{
		Writer:    "GoSX assetpipe ibl",
		KeyValues: keyValues,
	})
}

// DecodeCubeKTX2 reads an RGBA16F cubemap back into a mip chain. The IBL
// tests use it to check what the writer produced.
func DecodeCubeKTX2(data []byte) (Chain, error) {
	img, err := ktx2.Parse(data)
	if err != nil {
		return nil, err
	}
	if img.Format != ktx2.VkFormatR16G16B16A16Sfloat {
		return nil, fmt.Errorf("ibl: expected RGBA16F, got vkFormat %d", img.Format)
	}
	if img.Faces != 6 {
		return nil, fmt.Errorf("ibl: expected 6 faces, got %d", img.Faces)
	}
	chain := make(Chain, len(img.Levels))
	for level, mip := range img.Levels {
		size := mip.Width
		cube := NewCube(size)
		bytesPerFace := size * size * 8
		if len(mip.Bytes) < bytesPerFace*6 {
			return nil, fmt.Errorf("ibl: level %d holds %d bytes, want %d", level, len(mip.Bytes), bytesPerFace*6)
		}
		for face := 0; face < 6; face++ {
			base := face * bytesPerFace
			target := cube.Faces[face]
			for texel := 0; texel < size*size; texel++ {
				off := base + texel*8
				target[texel*3+0] = hdrimage.HalfToFloat32(binary.LittleEndian.Uint16(mip.Bytes[off+0:]))
				target[texel*3+1] = hdrimage.HalfToFloat32(binary.LittleEndian.Uint16(mip.Bytes[off+2:]))
				target[texel*3+2] = hdrimage.HalfToFloat32(binary.LittleEndian.Uint16(mip.Bytes[off+4:]))
			}
		}
		chain[level] = cube
	}
	return chain, nil
}
