package assetpipe

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"

	"m31labs.dev/gosx/assetpipe/meshopt"
)

// MeshoptExtension is the glTF extension a meshopt-compressed GLB requires.
const MeshoptExtension = "EXT_meshopt_compression"

// MeshoptStats records what compressing one GLB did.
type MeshoptStats struct {
	// Views counts buffer views that were compressed, and Skipped the mesh views
	// left as they were because they did not fit a codec.
	Views, Skipped int
	// BytesBefore and BytesAfter are the whole GLB sizes.
	BytesBefore, BytesAfter int
	// ViewBytesBefore and ViewBytesAfter cover only the compressed views.
	ViewBytesBefore, ViewBytesAfter int
}

type meshoptRole int

const (
	roleNone meshoptRole = iota
	roleAttribute
	roleIndex
	roleMixed
)

// compressMeshopt rewrites the mesh buffer views of a GLB with
// EXT_meshopt_compression. Vertex attribute views use the vertex codec and
// triangle index views the index codec; images, animation, skin and any view a
// codec cannot hold keep their bytes. The GLB needs one embedded buffer, which
// is what the asset build writes.
//
// The result lists the extension as required. Each compressed view points at a
// second buffer that has a byte length and no data, as gltfpack writes it: the
// uncompressed fallback the extension allows but the loader never reads.
func compressMeshopt(glb []byte) ([]byte, MeshoptStats, error) {
	stats := MeshoptStats{BytesBefore: len(glb)}
	jsonChunk, bin, err := splitGLBChunks(glb)
	if err != nil {
		return nil, stats, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(jsonChunk, &root); err != nil {
		return nil, stats, err
	}
	var buffers []map[string]json.RawMessage
	var views, accessors, meshes []map[string]json.RawMessage
	for key, target := range map[string]*[]map[string]json.RawMessage{"buffers": &buffers, "bufferViews": &views, "accessors": &accessors, "meshes": &meshes} {
		if raw, ok := root[key]; ok {
			if err := json.Unmarshal(raw, target); err != nil {
				return nil, stats, fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	if len(buffers) != 1 || len(views) == 0 {
		return glb, stats, fmt.Errorf("meshopt needs a GLB with one embedded buffer and buffer views")
	}
	if _, has := buffers[0]["uri"]; has {
		return glb, stats, fmt.Errorf("meshopt needs an embedded buffer")
	}
	for _, view := range views {
		if _, done := view["extensions"]; done {
			if string(view["extensions"]) != "null" && containsKey(view["extensions"], MeshoptExtension) {
				return glb, stats, fmt.Errorf("the GLB is already meshopt compressed")
			}
		}
	}

	type accessorInfo struct {
		view, offset, count, componentType int
		kind                               string
	}
	infos := make([]accessorInfo, len(accessors))
	for i, a := range accessors {
		infos[i] = accessorInfo{view: -1}
		var view *int
		_ = unmarshalField(a, "bufferView", &view)
		if view == nil || len(a["sparse"]) > 0 {
			continue
		}
		infos[i].view = *view
		_ = unmarshalField(a, "byteOffset", &infos[i].offset)
		_ = unmarshalField(a, "count", &infos[i].count)
		_ = unmarshalField(a, "componentType", &infos[i].componentType)
		_ = unmarshalField(a, "type", &infos[i].kind)
	}

	roles := make([]meshoptRole, len(views))
	owners := make([][]int, len(views)) // accessors reading each view
	mark := func(accessor int, role meshoptRole) {
		if accessor < 0 || accessor >= len(infos) || infos[accessor].view < 0 || infos[accessor].view >= len(views) {
			return
		}
		v := infos[accessor].view
		owners[v] = append(owners[v], accessor)
		if roles[v] == roleNone || roles[v] == role {
			roles[v] = role
		} else {
			roles[v] = roleMixed
		}
	}
	for _, m := range meshes {
		var prims []struct {
			Attributes map[string]int   `json:"attributes"`
			Indices    *int             `json:"indices"`
			Mode       *int             `json:"mode"`
			Targets    []map[string]int `json:"targets"`
		}
		_ = unmarshalField(m, "primitives", &prims)
		for _, p := range prims {
			for _, a := range p.Attributes {
				mark(a, roleAttribute)
			}
			for _, target := range p.Targets {
				for _, a := range target {
					mark(a, roleAttribute)
				}
			}
			if p.Indices != nil {
				if p.Mode == nil || *p.Mode == 4 {
					mark(*p.Indices, roleIndex)
				} else {
					mark(*p.Indices, roleMixed)
				}
			}
		}
	}
	// A view used outside meshes (animation, skins, images) must stay as it is.
	used := make([]int, len(views))
	for _, info := range infos {
		if info.view >= 0 && info.view < len(views) {
			used[info.view]++
		}
	}

	type segment struct {
		view       int
		compressed []byte
		mode       string
		count      int
		stride     int
		rawLength  int
	}
	var segments []segment
	compressedView := make([]bool, len(views))
	for i, view := range views {
		if roles[i] != roleAttribute && roles[i] != roleIndex {
			continue
		}
		if len(owners[i]) != used[i] {
			stats.Skipped++
			continue
		}
		var off, length, stride, target int
		_ = unmarshalField(view, "byteOffset", &off)
		_ = unmarshalField(view, "byteLength", &length)
		_ = unmarshalField(view, "byteStride", &stride)
		_ = unmarshalField(view, "target", &target)
		if off < 0 || length <= 0 || off+length > len(bin) {
			stats.Skipped++
			continue
		}
		data := bin[off : off+length]
		first := infos[owners[i][0]]
		elem := componentBytes(first.componentType) * kindComponents(first.kind)
		if elem == 0 {
			stats.Skipped++
			continue
		}
		fits := true
		for _, ai := range owners[i] {
			a := infos[ai]
			if a.offset+a.count*maxInt(stride, componentBytes(a.componentType)*kindComponents(a.kind)) > length {
				fits = false
			}
		}
		switch roles[i] {
		case roleAttribute:
			if stride == 0 {
				stride = elem
			}
			if !fits || stride > 256 || length%stride != 0 {
				stats.Skipped++
				continue
			}
			// Every accessor must read whole elements from the view's start.
			for _, ai := range owners[i] {
				if infos[ai].offset%4 != 0 {
					fits = false
				}
			}
			if !fits {
				stats.Skipped++
				continue
			}
			count := length / stride
			if stride%4 != 0 {
				// The codec needs a stride of 4 bytes. Pad each element with zeros,
				// as the glTF alignment rule asks of vertex attributes anyway.
				padded := (stride + 3) &^ 3
				if padded > 256 {
					stats.Skipped++
					continue
				}
				repacked := make([]byte, count*padded)
				for v := 0; v < count; v++ {
					copy(repacked[v*padded:], data[v*stride:(v+1)*stride])
				}
				data, stride, length = repacked, padded, len(repacked)
			}
			enc, err := meshopt.EncodeVertexBuffer(data, count, stride)
			if err != nil {
				stats.Skipped++
				continue
			}
			segments = append(segments, segment{view: i, compressed: enc, mode: "ATTRIBUTES", count: count, stride: stride, rawLength: length})
		case roleIndex:
			if stride != 0 || len(owners[i]) != 1 || first.offset != 0 || first.kind != "SCALAR" ||
				(first.componentType != 5123 && first.componentType != 5125) || first.count%3 != 0 || first.count*elem != length {
				stats.Skipped++
				continue
			}
			indices := make([]uint32, first.count)
			maxIndex := uint32(0)
			for k := range indices {
				if elem == 2 {
					indices[k] = uint32(binary.LittleEndian.Uint16(data[k*2:]))
				} else {
					indices[k] = binary.LittleEndian.Uint32(data[k*4:])
				}
				if indices[k] > maxIndex {
					maxIndex = indices[k]
				}
			}
			enc, err := meshopt.EncodeIndexBuffer(indices, int(maxIndex)+1)
			if err != nil {
				stats.Skipped++
				continue
			}
			segments = append(segments, segment{view: i, compressed: enc, mode: "TRIANGLES", count: first.count, stride: elem, rawLength: length})
		}
		compressedView[i] = true
	}
	if len(segments) == 0 {
		return glb, stats, nil
	}

	// New binary chunk: the views that stay, then the compressed segments.
	var newBin []byte
	align := func() {
		for len(newBin)%4 != 0 {
			newBin = append(newBin, 0)
		}
	}
	order := make([]int, 0, len(views))
	for i := range views {
		if !compressedView[i] {
			order = append(order, i)
		}
	}
	sort.Ints(order)
	for _, i := range order {
		var off, length int
		_ = unmarshalField(views[i], "byteOffset", &off)
		_ = unmarshalField(views[i], "byteLength", &length)
		if off < 0 || length < 0 || off+length > len(bin) {
			return glb, stats, fmt.Errorf("bufferView %d lies outside the binary chunk", i)
		}
		align()
		views[i]["byteOffset"] = mustJSON(len(newBin))
		newBin = append(newBin, bin[off:off+length]...)
	}
	fallback := 0
	for _, seg := range segments {
		align()
		offset := len(newBin)
		newBin = append(newBin, seg.compressed...)
		ext, _ := json.Marshal(map[string]any{
			"buffer": 0, "byteOffset": offset, "byteLength": len(seg.compressed),
			"byteStride": seg.stride, "mode": seg.mode, "count": seg.count,
		})
		views[seg.view]["buffer"] = mustJSON(1)
		views[seg.view]["byteOffset"] = mustJSON(fallback)
		views[seg.view]["byteLength"] = mustJSON(seg.rawLength)
		if seg.mode == "ATTRIBUTES" {
			views[seg.view]["byteStride"] = mustJSON(seg.stride)
		}
		views[seg.view]["extensions"] = mustJSON(map[string]json.RawMessage{MeshoptExtension: ext})
		fallback += (seg.rawLength + 3) &^ 3
		stats.Views++
		stats.ViewBytesBefore += seg.rawLength
		stats.ViewBytesAfter += len(seg.compressed)
	}
	align()
	buffers[0]["byteLength"] = mustJSON(len(newBin))
	buffers = append(buffers, map[string]json.RawMessage{
		"byteLength": mustJSON(fallback),
		"extensions": mustJSON(map[string]any{MeshoptExtension: map[string]bool{"fallback": true}}),
	})
	root["buffers"] = mustJSON(buffers)
	root["bufferViews"] = mustJSON(views)
	for _, key := range []string{"extensionsUsed", "extensionsRequired"} {
		var list []string
		if raw, ok := root[key]; ok {
			_ = json.Unmarshal(raw, &list)
		}
		if !contains(list, MeshoptExtension) {
			list = append(list, MeshoptExtension)
		}
		root[key] = mustJSON(list)
	}
	out, err := assembleGLB(root, newBin)
	if err != nil {
		return nil, stats, err
	}
	stats.BytesAfter = len(out)
	return out, stats, nil
}

func assembleGLB(root map[string]json.RawMessage, bin []byte) ([]byte, error) {
	chunk, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	for len(chunk)%4 != 0 {
		chunk = append(chunk, ' ')
	}
	for len(bin)%4 != 0 {
		bin = append(bin, 0)
	}
	total := 12 + 8 + len(chunk)
	if len(bin) > 0 {
		total += 8 + len(bin)
	}
	out := make([]byte, 0, total)
	out = append(out, 'g', 'l', 'T', 'F')
	out = binary.LittleEndian.AppendUint32(out, 2)
	out = binary.LittleEndian.AppendUint32(out, uint32(total))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(chunk)))
	out = binary.LittleEndian.AppendUint32(out, 0x4E4F534A)
	out = append(out, chunk...)
	if len(bin) > 0 {
		out = binary.LittleEndian.AppendUint32(out, uint32(len(bin)))
		out = binary.LittleEndian.AppendUint32(out, 0x004E4942)
		out = append(out, bin...)
	}
	return out, nil
}

func unmarshalField(m map[string]json.RawMessage, key string, v any) error {
	raw, ok := m[key]
	if !ok {
		return fmt.Errorf("no %s", key)
	}
	return json.Unmarshal(raw, v)
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

func containsKey(raw json.RawMessage, key string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	_, ok := m[key]
	return ok
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func kindComponents(kind string) int {
	switch kind {
	case "SCALAR":
		return 1
	case "VEC2":
		return 2
	case "VEC3":
		return 3
	case "VEC4":
		return 4
	case "MAT2":
		return 4
	case "MAT3":
		return 9
	case "MAT4":
		return 16
	}
	return 0
}
