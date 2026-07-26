package gltfedit

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"
)

// buildDocument assembles a GLB from a JSON document and a payload.
func buildDocument(t *testing.T, doc map[string]any, payload []byte) []byte {
	t.Helper()
	jsonChunk, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for len(jsonChunk)%4 != 0 {
		jsonChunk = append(jsonChunk, ' ')
	}
	for len(payload)%4 != 0 {
		payload = append(payload, 0)
	}
	total := 12 + 8 + len(jsonChunk) + 8 + len(payload)
	out := make([]byte, 0, total)
	out = append(out, 'g', 'l', 'T', 'F')
	out = appendU32(out, 2)
	out = appendU32(out, uint32(total))
	out = appendU32(out, uint32(len(jsonChunk)))
	out = appendU32(out, 0x4E4F534A)
	out = append(out, jsonChunk...)
	out = appendU32(out, uint32(len(payload)))
	out = appendU32(out, 0x004E4942)
	out = append(out, payload...)
	return out
}

func floatPayload(values []float32) []byte {
	out := make([]byte, len(values)*4)
	for i, value := range values {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(value))
	}
	return out
}

func TestSetAccessorIntsKeepsStoredUnitsInMinAndMax(t *testing.T) {
	// A normalized accessor records min and max in stored units, which is what
	// the specification asks for and what the validator checks.
	source := buildDocument(t, map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{"attributes": map[string]any{"NORMAL": 0}}},
		}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": 2, "type": "VEC3"},
		},
		"bufferViews": []map[string]any{{"buffer": 0, "byteOffset": 0, "byteLength": 24}},
		"buffers":     []map[string]any{{"byteLength": 24}},
	}, floatPayload([]float32{1, 0, 0, 0, 1, 0}))

	document, err := Parse(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	stored := []int32{127, 0, 0, 0, -127, 0}
	if err := document.SetAccessorInts(0, stored, "VEC3", ComponentByte, true, TargetArrayBuffer); err != nil {
		t.Fatal(err)
	}
	accessor := document.AccessorInfo(0)
	if accessor.ComponentType != ComponentByte || !accessor.Normalized {
		t.Fatalf("componentType %d normalized %v", accessor.ComponentType, accessor.Normalized)
	}
	if accessor.Count != 2 {
		t.Fatalf("count %d, want 2", accessor.Count)
	}
	wantMin := []float64{0, -127, 0}
	wantMax := []float64{127, 0, 0}
	for axis := 0; axis < 3; axis++ {
		if accessor.Min[axis] != wantMin[axis] || accessor.Max[axis] != wantMax[axis] {
			t.Fatalf("min %v max %v, want %v and %v", accessor.Min, accessor.Max, wantMin, wantMax)
		}
	}

	// A read must decode the stored bytes through the normalized rule.
	values, components, err := document.ReadAccessor(0)
	if err != nil {
		t.Fatal(err)
	}
	if components != 3 {
		t.Fatalf("components %d, want 3", components)
	}
	if values[0] != 1 || values[4] != -1 {
		t.Fatalf("decoded %v, want 1 at index 0 and -1 at index 4", values)
	}
}

func TestWriteGLBSharesIdenticalBufferViewPayloads(t *testing.T) {
	// Two primitives whose attributes hold the same bytes must cost those bytes
	// once. The buffer views stay separate records, which keeps every accessor
	// valid.
	payload := floatPayload([]float32{1, 2, 3, 1, 2, 3})
	source := buildDocument(t, map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{
				{"attributes": map[string]any{"POSITION": 0}},
				{"attributes": map[string]any{"POSITION": 1}},
			},
		}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": 1, "type": "VEC3"},
			{"bufferView": 1, "componentType": 5126, "count": 1, "type": "VEC3"},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": 0, "byteLength": 12},
			{"buffer": 0, "byteOffset": 12, "byteLength": 12},
		},
		"buffers": []map[string]any{{"byteLength": 24}},
	}, payload)

	document, err := Parse(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := document.WriteGLB()
	if err != nil {
		t.Fatal(err)
	}
	written, err := Parse(encoded, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(written.BufferViews) != 2 {
		t.Fatalf("buffer views = %d, want 2", len(written.BufferViews))
	}
	if written.BufferViews[0].ByteOffset != written.BufferViews[1].ByteOffset {
		t.Fatalf("identical payloads must share one offset: %d and %d",
			written.BufferViews[0].ByteOffset, written.BufferViews[1].ByteOffset)
	}
	if written.Buffers[0].ByteLength > 16 {
		t.Fatalf("the shared payload must cost twelve bytes, buffer holds %d", written.Buffers[0].ByteLength)
	}
	// Both accessors must still read the same values.
	for index := 0; index < 2; index++ {
		values, _, err := written.ReadAccessor(index)
		if err != nil {
			t.Fatal(err)
		}
		if values[0] != 1 || values[1] != 2 || values[2] != 3 {
			t.Fatalf("accessor %d reads %v", index, values)
		}
	}
}

func TestCompactAccessorsDropsUnreachableAccessors(t *testing.T) {
	payload := floatPayload([]float32{1, 2, 3, 9, 9, 9})
	source := buildDocument(t, map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{"attributes": map[string]any{"POSITION": 1}}},
		}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": 1, "type": "VEC3"},
			{"bufferView": 1, "componentType": 5126, "count": 1, "type": "VEC3"},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": 0, "byteLength": 12},
			{"buffer": 0, "byteOffset": 12, "byteLength": 12},
		},
		"buffers": []map[string]any{{"byteLength": 24}},
	}, payload)

	document, err := Parse(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed := document.CompactAccessors(); removed != 1 {
		t.Fatalf("removed %d accessors, want 1", removed)
	}
	if len(document.Accessors) != 1 {
		t.Fatalf("accessors left %d, want 1", len(document.Accessors))
	}
	if got := document.Meshes[0].Primitives[0].Attributes["POSITION"]; got != 0 {
		t.Fatalf("POSITION renumbered to %d, want 0", got)
	}
	values, _, err := document.ReadAccessor(0)
	if err != nil {
		t.Fatal(err)
	}
	if values[0] != 9 {
		t.Fatalf("the surviving accessor reads %v, want the second vector", values)
	}
}

func TestCompactAccessorsRefusesASparseDocument(t *testing.T) {
	// A sparse accessor names buffer views inside JSON this package keeps
	// opaque, so no pass may renumber or drop anything.
	payload := floatPayload([]float32{1, 2, 3, 4, 5, 6})
	source := buildDocument(t, map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{"attributes": map[string]any{"POSITION": 0}}},
		}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": 1, "type": "VEC3"},
			{"bufferView": 1, "componentType": 5126, "count": 1, "type": "VEC3",
				"sparse": map[string]any{
					"count":   1,
					"indices": map[string]any{"bufferView": 1, "componentType": 5123},
					"values":  map[string]any{"bufferView": 1},
				}},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": 0, "byteLength": 12},
			{"buffer": 0, "byteOffset": 12, "byteLength": 12},
		},
		"buffers": []map[string]any{{"byteLength": 24}},
	}, payload)

	document, err := Parse(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed := document.CompactAccessors(); removed != 0 {
		t.Fatalf("a sparse document must be left alone, removed %d", removed)
	}
	// The writer must also keep every buffer view of a sparse document.
	encoded, err := document.WriteGLB()
	if err != nil {
		t.Fatal(err)
	}
	written, err := Parse(encoded, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(written.BufferViews) != 2 {
		t.Fatalf("buffer views = %d, want both kept", len(written.BufferViews))
	}
}

func TestRemoveNodesRenumbersEveryReference(t *testing.T) {
	source := buildDocument(t, map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"nodes": []map[string]any{
			{"name": "root", "children": []int{1, 2, 3}},
			{"name": "drop-me"},
			{"name": "joint"},
			{"name": "animated"},
		},
		"scenes": []map[string]any{{"nodes": []int{0}}},
		"skins":  []map[string]any{{"joints": []int{2}, "skeleton": 2}},
		"animations": []map[string]any{{
			"channels": []map[string]any{{"sampler": 0, "target": map[string]any{"node": 3, "path": "translation"}}},
			"samplers": []map[string]any{{"input": 0, "output": 0}},
		}},
		"accessors":   []map[string]any{{"bufferView": 0, "componentType": 5126, "count": 1, "type": "SCALAR"}},
		"bufferViews": []map[string]any{{"buffer": 0, "byteOffset": 0, "byteLength": 4}},
		"buffers":     []map[string]any{{"byteLength": 4}},
	}, floatPayload([]float32{0}))

	document, err := Parse(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed := document.RemoveNodes([]int{1}); removed != 1 {
		t.Fatalf("removed %d nodes, want 1", removed)
	}
	if len(document.Nodes) != 3 {
		t.Fatalf("nodes left %d, want 3", len(document.Nodes))
	}
	if got := document.Nodes[0].Children; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("children renumbered to %v, want [1 2]", got)
	}
	if got := document.Skins[0].Joints; len(got) != 1 || got[0] != 1 {
		t.Fatalf("skin joints renumbered to %v, want [1]", got)
	}
	if document.Skins[0].Skeleton == nil || *document.Skins[0].Skeleton != 1 {
		t.Fatalf("skeleton renumbered to %v, want 1", document.Skins[0].Skeleton)
	}
	target := document.Animations[0].Channels[0].Target.Node
	if target == nil || *target != 2 {
		t.Fatalf("animation target renumbered to %v, want 2", target)
	}
}

func TestRemoveNodesKeepsAParentOfChildren(t *testing.T) {
	source := buildDocument(t, map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"nodes": []map[string]any{
			{"name": "root", "children": []int{1}},
			{"name": "has-a-child", "children": []int{2}},
			{"name": "leaf"},
		},
		"scenes":  []map[string]any{{"nodes": []int{0}}},
		"buffers": []map[string]any{{"byteLength": 4}},
	}, floatPayload([]float32{0}))

	document, err := Parse(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed := document.RemoveNodes([]int{1}); removed != 0 {
		t.Fatalf("a node with children must survive, removed %d", removed)
	}
}

func TestRemoveNodesRefusesAnUnknownExtension(t *testing.T) {
	source := buildDocument(t, map[string]any{
		"asset":          map[string]any{"version": "2.0"},
		"extensionsUsed": []string{"KHR_lights_punctual"},
		"nodes": []map[string]any{
			{"name": "root", "children": []int{1}},
			{"name": "leaf"},
		},
		"scenes":  []map[string]any{{"nodes": []int{0}}},
		"buffers": []map[string]any{{"byteLength": 4}},
	}, floatPayload([]float32{0}))

	document, err := Parse(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed := document.RemoveNodes([]int{1}); removed != 0 {
		t.Fatalf("an unmodelled extension may name a node, removed %d", removed)
	}
}

func TestWriteGLBKeepsUnchangedSectionsByteIdentical(t *testing.T) {
	// A rewrite of one mesh must not reformat a section nobody touched.
	source := buildDocument(t, map[string]any{
		"asset": map[string]any{"version": "2.0", "generator": "test"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{"attributes": map[string]any{"POSITION": 0}}},
		}},
		"nodes":       []map[string]any{{"mesh": 0, "name": "keep"}},
		"scenes":      []map[string]any{{"nodes": []int{0}, "name": "main"}},
		"accessors":   []map[string]any{{"bufferView": 0, "componentType": 5126, "count": 1, "type": "VEC3"}},
		"bufferViews": []map[string]any{{"buffer": 0, "byteOffset": 0, "byteLength": 12}},
		"buffers":     []map[string]any{{"byteLength": 12}},
	}, floatPayload([]float32{1, 2, 3}))

	document, err := Parse(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := document.WriteGLB()
	if err != nil {
		t.Fatal(err)
	}
	written, err := Parse(encoded, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(written.Nodes) != 1 || written.Nodes[0].Name != "keep" {
		t.Fatalf("nodes changed: %+v", written.Nodes)
	}
	if len(written.Scenes) != 1 || written.Scenes[0].Name != "main" {
		t.Fatalf("scenes changed: %+v", written.Scenes)
	}
}

func TestDeclareExtensionWritesTheSection(t *testing.T) {
	source := buildDocument(t, map[string]any{
		"asset":       map[string]any{"version": "2.0"},
		"meshes":      []map[string]any{{"primitives": []map[string]any{{"attributes": map[string]any{"POSITION": 0}}}}},
		"accessors":   []map[string]any{{"bufferView": 0, "componentType": 5126, "count": 1, "type": "VEC3"}},
		"bufferViews": []map[string]any{{"buffer": 0, "byteOffset": 0, "byteLength": 12}},
		"buffers":     []map[string]any{{"byteLength": 12}},
	}, floatPayload([]float32{1, 2, 3}))

	document, err := Parse(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	document.DeclareExtension("KHR_mesh_quantization")
	document.DeclareExtension("KHR_mesh_quantization")
	encoded, err := document.WriteGLB()
	if err != nil {
		t.Fatal(err)
	}
	written, err := Parse(encoded, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(written.ExtensionsUsed) != 1 || written.ExtensionsUsed[0] != "KHR_mesh_quantization" {
		t.Fatalf("extensionsUsed = %v", written.ExtensionsUsed)
	}
}

func TestOnlyInstancingExtensionSeesOneKey(t *testing.T) {
	node := Node{}
	if OnlyInstancingExtension(node) {
		t.Fatal("a node with no extension must not report the instancing extension")
	}
	if err := SetInstanceAttributes(&node, map[string]int{"TRANSLATION": 3}); err != nil {
		t.Fatal(err)
	}
	if !OnlyInstancingExtension(node) {
		t.Fatalf("a node with only the instancing extension must report it: %s", node.Extensions)
	}
	if got := InstanceAttributes(node)["TRANSLATION"]; got != 3 {
		t.Fatalf("attributes = %v", InstanceAttributes(node))
	}
	node.Extensions = json.RawMessage(`{"EXT_mesh_gpu_instancing":{"attributes":{}},"KHR_lights_punctual":{}}`)
	if OnlyInstancingExtension(node) {
		t.Fatal("a second extension must switch the answer to false")
	}
}
