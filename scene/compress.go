package scene

import (
	"encoding/binary"
	"math"
)

// sceneChunkSize is the max float count per compression chunk.
const sceneChunkSize = 4096

// compressSceneIR walks the IR and compresses eligible float arrays in place.
// When previewBitWidth > 0, a lower-quality preview is also emitted for
// progressive loading and LOD.
func compressSceneIR(ir *SceneIR, bitWidth, previewBitWidth int) {
	for i := range ir.Points {
		if len(ir.Points[i].Positions) >= 2 {
			if previewBitWidth > 0 && previewBitWidth < bitWidth {
				ir.Points[i].PreviewPositions = compressFloat64Array(
					ir.Points[i].Positions, previewBitWidth,
				)
			}
			ir.Points[i].CompressedPositions = compressFloat64Array(
				ir.Points[i].Positions, bitWidth,
			)
			ir.Points[i].Positions = nil
		}
		if len(ir.Points[i].Sizes) >= 2 {
			if previewBitWidth > 0 && previewBitWidth < bitWidth {
				ir.Points[i].PreviewSizes = compressFloat64Array(
					ir.Points[i].Sizes, previewBitWidth,
				)
			}
			ir.Points[i].CompressedSizes = compressFloat64Array(
				ir.Points[i].Sizes, bitWidth,
			)
			ir.Points[i].Sizes = nil
		}
	}
	for i := range ir.InstancedMeshes {
		if len(ir.InstancedMeshes[i].Transforms) >= 2 {
			if previewBitWidth > 0 && previewBitWidth < bitWidth {
				ir.InstancedMeshes[i].PreviewTransforms = compressFloat64Array(
					ir.InstancedMeshes[i].Transforms, previewBitWidth,
				)
			}
			ir.InstancedMeshes[i].CompressedTransforms = compressFloat64Array(
				ir.InstancedMeshes[i].Transforms, bitWidth,
			)
			ir.InstancedMeshes[i].Transforms = nil
		}
	}
	for i := range ir.Animations {
		for j := range ir.Animations[i].Channels {
			ch := &ir.Animations[i].Channels[j]
			if len(ch.Times) >= 2 {
				if previewBitWidth > 0 && previewBitWidth < bitWidth {
					ch.PreviewTimes = compressFloat64Array(ch.Times, previewBitWidth)
				}
				ch.CompressedTimes = compressFloat64Array(ch.Times, bitWidth)
				ch.Times = nil
			}
			if len(ch.Values) >= 2 {
				if previewBitWidth > 0 && previewBitWidth < bitWidth {
					ch.PreviewValues = compressFloat64Array(ch.Values, previewBitWidth)
				}
				ch.CompressedValues = compressFloat64Array(ch.Values, bitWidth)
				ch.Values = nil
			}
		}
	}
}

// compressFloat64Array converts float64 data to scalar-quantized chunks.
// Uses per-chunk min/max scalar quantization: simple, fast to decompress in JS,
// and the only metadata is 2 floats (min, max) per chunk instead of a full
// rotation matrix.
func compressFloat64Array(data []float64, bitWidth int) []CompressedArray {
	if len(data) < 2 {
		return nil
	}

	var chunks []CompressedArray
	for offset := 0; offset < len(data); offset += sceneChunkSize {
		end := offset + sceneChunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk := data[offset:end]
		count := len(chunk)
		if count < 2 {
			break
		}

		// Find min/max for scalar quantization
		minVal := chunk[0]
		maxVal := chunk[0]
		for _, v := range chunk[1:] {
			if v < minVal {
				minVal = v
			}
			if v > maxVal {
				maxVal = v
			}
		}

		levels := (1 << uint(bitWidth)) - 1
		rangeVal := maxVal - minVal
		if rangeVal < 1e-12 {
			rangeVal = 1e-12
		}
		scale := float64(levels) / rangeVal

		// Quantize each value to b-bit index
		indices := make([]int, count)
		for i, v := range chunk {
			idx := int(math.Round((v - minVal) * scale))
			if idx < 0 {
				idx = 0
			}
			if idx > levels {
				idx = levels
			}
			indices[i] = idx
		}

		// Pack indices
		packed := make([]byte, packedSizeScene(count, bitWidth))
		packIndicesScene(packed, indices, bitWidth)

		// Encode min/max as the "norm" field (8 bytes: min as float32 + max as float32)
		var minMax [8]byte
		binary.LittleEndian.PutUint32(minMax[0:4], math.Float32bits(float32(minVal)))
		binary.LittleEndian.PutUint32(minMax[4:8], math.Float32bits(float32(maxVal)))

		chunks = append(chunks, CompressedArray{
			Packed:   packed,
			Norm:     float32(minVal),
			Dim:      count,
			BitWidth: bitWidth,
			Count:    count,
			MaxVal:   float32(maxVal),
		})
	}
	return chunks
}

// DecompressFloat64Array reconstructs a float64 slice from compressed chunks.
func DecompressFloat64Array(chunks []CompressedArray) []float64 {
	var result []float64
	for _, chunk := range chunks {
		if chunk.Count < 2 {
			continue
		}
		minVal := float64(chunk.Norm)
		maxVal := float64(chunk.MaxVal)
		levels := (1 << uint(chunk.BitWidth)) - 1
		step := (maxVal - minVal) / float64(levels)

		indices := make([]int, chunk.Count)
		unpackIndicesScene(indices, chunk.Packed, chunk.Count, chunk.BitWidth)

		for _, idx := range indices {
			result = append(result, minVal+float64(idx)*step)
		}
	}
	return result
}

// packedSizeScene returns bytes needed for count b-bit indices.
func packedSizeScene(count, bitWidth int) int {
	return (count*bitWidth + 7) / 8
}

// packIndicesScene packs b-bit indices into bytes.
func packIndicesScene(dst []byte, indices []int, bitWidth int) {
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

// unpackIndicesScene unpacks b-bit indices from bytes.
func unpackIndicesScene(indices []int, src []byte, count, bitWidth int) {
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
