package beachgen

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
)

type shipPart struct {
	name     string
	mesh     *geometry
	material int
}

// writeShipGLB preserves material and mesh names for cheap runtime sail morphs.
// Each node has its own quantization transform, decoded before vessel motion.
func writeShipGLB(parts []shipPart, materials []map[string]any) ([]byte, error) {
	var bin binaryBuilder
	var accessors, meshes, nodes []map[string]any
	var nodeIDs []int
	for i, part := range parts {
		g := part.mesh
		count := len(g.positions) / 3
		if count == 0 || count >= 65536 || len(g.normals) != len(g.positions) || len(g.uvs) != count*2 {
			return nil, fmt.Errorf("invalid ship part %s", part.name)
		}
		for _, index := range g.indices {
			if int(index) >= count {
				return nil, fmt.Errorf("invalid ship index in %s", part.name)
			}
		}
		pos, scale, translation := encodePositions(g.positions)
		views := []int{bin.addView(pos, 34962), bin.addView(encodeNormals(g.normals), 34962), bin.addView(encodeUVs(g.uvs), 34962), bin.addView(encodeIndices(g.indices), 34963)}
		a := len(accessors)
		accessors = append(accessors,
			map[string]any{"bufferView": views[0], "componentType": 5122, "normalized": true, "count": count, "type": "VEC3"},
			map[string]any{"bufferView": views[1], "componentType": 5120, "normalized": true, "count": count, "type": "VEC3"},
			map[string]any{"bufferView": views[2], "componentType": 5121, "normalized": true, "count": count, "type": "VEC2"},
			map[string]any{"bufferView": views[3], "componentType": 5123, "count": len(g.indices), "type": "SCALAR"})
		meshes = append(meshes, map[string]any{"name": part.name, "primitives": []any{map[string]any{"attributes": map[string]int{"POSITION": a, "NORMAL": a + 1, "TEXCOORD_0": a + 2}, "indices": a + 3, "material": part.material, "mode": 4}}})
		nodes = append(nodes, map[string]any{"name": part.name, "mesh": i, "scale": []float64{scale, scale, scale}, "translation": translation})
		nodeIDs = append(nodeIDs, i)
	}
	root := map[string]any{"asset": map[string]any{"version": "2.0", "generator": "beachgen"}, "scene": 0, "scenes": []any{map[string]any{"nodes": nodeIDs}},
		"nodes": nodes, "meshes": meshes, "materials": materials, "accessors": accessors, "bufferViews": bin.views, "buffers": []any{map[string]any{"byteLength": len(bin.data)}},
		"extensionsUsed": []string{"KHR_mesh_quantization"}, "extensionsRequired": []string{"KHR_mesh_quantization"}}
	data, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	for len(data)%4 != 0 {
		data = append(data, ' ')
	}
	for len(bin.data)%4 != 0 {
		bin.data = append(bin.data, 0)
	}
	out := new(bytes.Buffer)
	out.WriteString("glTF")
	for _, v := range []uint32{2, uint32(28 + len(data) + len(bin.data)), uint32(len(data)), 0x4E4F534A} {
		_ = binary.Write(out, binary.LittleEndian, v)
	}
	out.Write(data)
	_ = binary.Write(out, binary.LittleEndian, uint32(len(bin.data)))
	_ = binary.Write(out, binary.LittleEndian, uint32(0x004E4942))
	out.Write(bin.data)
	return out.Bytes(), nil
}
