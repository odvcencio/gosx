package beachgen

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
)

type embeddedImage struct {
	name string
	data []byte
	mime string // "image/png" when empty
}

type binaryBuilder struct {
	data  []byte
	views []map[string]any
}

func (b *binaryBuilder) addView(data []byte, target int) int {
	for len(b.data)%4 != 0 {
		b.data = append(b.data, 0)
	}
	view := map[string]any{"buffer": 0, "byteOffset": len(b.data), "byteLength": len(data)}
	if target != 0 {
		view["target"] = target
	}
	b.views = append(b.views, view)
	b.data = append(b.data, data...)
	return len(b.views) - 1
}

// writeGLB packs a quantized mesh and optional embedded PNGs into a minimal
// glTF 2.0 binary container. Its output is stable for identical inputs.
func writeGLB(mesh *geometry, material map[string]any, images []embeddedImage) ([]byte, error) {
	if mesh == nil || len(mesh.positions)%3 != 0 || len(mesh.normals) != len(mesh.positions) || len(mesh.uvs)*3 != len(mesh.positions)*2 {
		return nil, fmt.Errorf("invalid mesh attribute lengths")
	}
	vertexCount := len(mesh.positions) / 3
	if vertexCount == 0 || vertexCount >= 65536 {
		return nil, fmt.Errorf("mesh has %d vertices; UNSIGNED_SHORT indices require fewer than 65,536", vertexCount)
	}
	for _, index := range mesh.indices {
		if int(index) >= vertexCount {
			return nil, fmt.Errorf("index %d exceeds %d vertices", index, vertexCount)
		}
	}
	var bin binaryBuilder
	imageRecords := make([]map[string]any, 0, len(images))
	textureRecords := make([]map[string]any, 0, len(images))
	for index, embedded := range images {
		view := bin.addView(embedded.data, 0)
		mime := embedded.mime
		if mime == "" {
			mime = "image/png"
		}
		imageRecords = append(imageRecords, map[string]any{"name": embedded.name, "bufferView": view, "mimeType": mime})
		textureRecords = append(textureRecords, map[string]any{"sampler": 0, "source": index})
	}
	positionBytes, positionScale, positionTranslation := encodePositions(mesh.positions)
	positionView := bin.addView(positionBytes, 34962)
	normalView := bin.addView(encodeNormals(mesh.normals), 34962)
	uvView := bin.addView(encodeUVs(mesh.uvs), 34962)
	indexView := bin.addView(encodeIndices(mesh.indices), 34963)

	accessors := []map[string]any{
		{"bufferView": positionView, "componentType": 5122, "normalized": true, "count": vertexCount, "type": "VEC3"},
		{"bufferView": normalView, "componentType": 5120, "normalized": true, "count": vertexCount, "type": "VEC3"},
		{"bufferView": uvView, "componentType": 5121, "normalized": true, "count": vertexCount, "type": "VEC2"},
		{"bufferView": indexView, "componentType": 5123, "count": len(mesh.indices), "type": "SCALAR"},
	}
	primitive := map[string]any{"attributes": map[string]int{"POSITION": 0, "NORMAL": 1, "TEXCOORD_0": 2}, "indices": 3, "material": 0, "mode": 4}
	root := map[string]any{
		"asset":              map[string]any{"version": "2.0", "generator": "beachgen"},
		"scene":              0,
		"scenes":             []any{map[string]any{"nodes": []int{0}}},
		"nodes":              []any{map[string]any{"mesh": 0, "scale": []float64{positionScale, positionScale, positionScale}, "translation": positionTranslation}},
		"meshes":             []any{map[string]any{"primitives": []any{primitive}}},
		"materials":          []any{material},
		"accessors":          accessors,
		"bufferViews":        bin.views,
		"buffers":            []any{map[string]any{"byteLength": len(bin.data)}},
		"extensionsUsed":     []string{"KHR_mesh_quantization"},
		"extensionsRequired": []string{"KHR_mesh_quantization"},
	}
	if len(images) != 0 {
		root["images"] = imageRecords
		root["textures"] = textureRecords
		root["samplers"] = []any{map[string]any{"magFilter": 9729, "minFilter": 9729, "wrapS": 10497, "wrapT": 10497}}
		root["extensionsUsed"] = []string{"KHR_mesh_quantization", "KHR_texture_transform"}
	}
	jsonData, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	for len(jsonData)%4 != 0 {
		jsonData = append(jsonData, ' ')
	}
	for len(bin.data)%4 != 0 {
		bin.data = append(bin.data, 0)
	}
	const jsonChunkType, binChunkType = uint32(0x4E4F534A), uint32(0x004E4942)
	totalLength := 12 + 8 + len(jsonData) + 8 + len(bin.data)
	out := bytes.NewBuffer(make([]byte, 0, totalLength))
	out.WriteString("glTF")
	_ = binary.Write(out, binary.LittleEndian, uint32(2))
	_ = binary.Write(out, binary.LittleEndian, uint32(totalLength))
	_ = binary.Write(out, binary.LittleEndian, uint32(len(jsonData)))
	_ = binary.Write(out, binary.LittleEndian, jsonChunkType)
	_, _ = out.Write(jsonData)
	_ = binary.Write(out, binary.LittleEndian, uint32(len(bin.data)))
	_ = binary.Write(out, binary.LittleEndian, binChunkType)
	_, _ = out.Write(bin.data)
	return out.Bytes(), nil
}

func encodePositions(values []float64) ([]byte, float64, []float64) {
	low := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	high := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for i := 0; i+2 < len(values); i += 3 {
		for axis := 0; axis < 3; axis++ {
			low[axis] = math.Min(low[axis], values[i+axis])
			high[axis] = math.Max(high[axis], values[i+axis])
		}
	}
	extent := math.Max(high[0]-low[0], math.Max(high[1]-low[1], high[2]-low[2]))
	if extent <= 0 {
		extent = 1
	}
	step := extent / 65534
	scale := step * 32767
	translation := []float64{low[0] + scale, low[1] + scale, low[2] + scale}
	out := make([]byte, len(values)*2)
	for i := 0; i+2 < len(values); i += 3 {
		for axis := 0; axis < 3; axis++ {
			q := int(math.Round((values[i+axis] - low[axis]) / step))
			q = int(clamp(float64(q), 0, 65534))
			binary.LittleEndian.PutUint16(out[(i+axis)*2:], uint16(int16(q-32767)))
		}
	}
	return out, scale, translation
}

func encodeNormals(values []float64) []byte {
	out := make([]byte, len(values))
	for i, value := range values {
		q := int(math.Round(clamp(value, -1, 1) * 127))
		out[i] = byte(int8(q))
	}
	return out
}

func encodeUVs(values []float64) []byte {
	out := make([]byte, len(values))
	for i, value := range values {
		q := uint8(math.Round(clamp(value, 0, 1) * 255))
		out[i] = q
	}
	return out
}

func encodeIndices(values []uint16) []byte {
	out := make([]byte, len(values)*2)
	for i, value := range values {
		binary.LittleEndian.PutUint16(out[i*2:], value)
	}
	return out
}
