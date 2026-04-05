package scene

import (
	"sync"

	"github.com/odvcencio/gosx/quant"
)

// sceneQuantSeed is a fixed seed for scene vertex compression.
// Distinct from the CRDT seed. Changing this is a breaking change for
// any client that has cached compressed scenes.
const sceneQuantSeed int64 = 0x676f73785f336478 // "gosx_3dx"

// sceneChunkSize is the max float count per compression chunk.
// Kept small so the per-chunk rotation matrix (dim x dim) is cheap to compute.
// 256 floats yields a 256x256 rotation matrix (~64K elements), which balances
// compression quality against quantizer construction cost.
const sceneChunkSize = 256

var sceneQuantCache sync.Map // key: sceneQuantCacheKey -> *quant.Quantizer

type sceneQuantCacheKey struct {
	dim  int
	bits int
}

func sceneQuantizer(dim, bitWidth int) *quant.Quantizer {
	key := sceneQuantCacheKey{dim, bitWidth}
	if q, ok := sceneQuantCache.Load(key); ok {
		return q.(*quant.Quantizer)
	}
	q := quant.NewWithSeed(dim, bitWidth, sceneQuantSeed)
	actual, _ := sceneQuantCache.LoadOrStore(key, q)
	return actual.(*quant.Quantizer)
}

// compressSceneIR walks the IR and compresses eligible float arrays in place.
func compressSceneIR(ir *SceneIR, bitWidth int) {
	for i := range ir.Points {
		if len(ir.Points[i].Positions) >= 2 {
			ir.Points[i].CompressedPositions = compressFloat64Array(
				ir.Points[i].Positions, bitWidth,
			)
			ir.Points[i].Positions = nil
		}
		if len(ir.Points[i].Sizes) >= 2 {
			ir.Points[i].CompressedSizes = compressFloat64Array(
				ir.Points[i].Sizes, bitWidth,
			)
			ir.Points[i].Sizes = nil
		}
	}
	for i := range ir.InstancedMeshes {
		if len(ir.InstancedMeshes[i].Transforms) >= 2 {
			ir.InstancedMeshes[i].CompressedTransforms = compressFloat64Array(
				ir.InstancedMeshes[i].Transforms, bitWidth,
			)
			ir.InstancedMeshes[i].Transforms = nil
		}
	}
}

// compressFloat64Array converts float64 data to float32, then quantizes in chunks.
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
		dim := len(chunk)
		if dim < 2 {
			// Remaining single element: cannot compress (quant requires dim >= 2).
			break
		}

		f32 := make([]float32, dim)
		for i, v := range chunk {
			f32[i] = float32(v)
		}

		q := sceneQuantizer(dim, bitWidth)
		packed, norm := q.Quantize(f32)
		chunks = append(chunks, CompressedArray{
			Packed:   packed,
			Norm:     norm,
			Dim:      dim,
			BitWidth: bitWidth,
			Count:    dim,
		})
	}
	return chunks
}

// DecompressFloat64Array reconstructs a float64 slice from compressed chunks.
// Exposed for testing; not part of the client-facing API.
func DecompressFloat64Array(chunks []CompressedArray) []float64 {
	var result []float64
	for _, chunk := range chunks {
		if chunk.Dim < 2 {
			continue
		}
		q := sceneQuantizer(chunk.Dim, chunk.BitWidth)
		unit := q.Dequantize(chunk.Packed)
		for i := range unit {
			result = append(result, float64(unit[i]*chunk.Norm))
		}
	}
	return result
}
