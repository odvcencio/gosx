package gltfedit

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"
)

func buildGLB(t *testing.T, doc map[string]any, payload []byte) []byte {
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
	total := 12 + 8 + len(jsonChunk)
	if len(payload) > 0 {
		total += 8 + len(payload)
	}
	var out bytes.Buffer
	out.WriteString("glTF")
	binary.Write(&out, binary.LittleEndian, uint32(2))
	binary.Write(&out, binary.LittleEndian, uint32(total))
	binary.Write(&out, binary.LittleEndian, uint32(len(jsonChunk)))
	binary.Write(&out, binary.LittleEndian, uint32(0x4E4F534A))
	out.Write(jsonChunk)
	if len(payload) > 0 {
		binary.Write(&out, binary.LittleEndian, uint32(len(payload)))
		binary.Write(&out, binary.LittleEndian, uint32(0x004E4942))
		out.Write(payload)
	}
	return out.Bytes()
}

// triangleGLB writes one triangle with positions, normalized byte colours and
// an unused buffer view the writer should drop.
func triangleGLB(t *testing.T) []byte {
	t.Helper()
	var bin bytes.Buffer
	positions := []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}
	for _, value := range positions {
		binary.Write(&bin, binary.LittleEndian, value)
	}
	colourOffset := bin.Len()
	bin.Write([]byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255})
	indexOffset := bin.Len()
	bin.Write([]byte{0, 1, 2, 0})
	unusedOffset := bin.Len()
	bin.Write(make([]byte, 64))

	doc := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{
				"attributes": map[string]any{"POSITION": 0, "COLOR_0": 1},
				"indices":    2,
				"mode":       4,
			}},
		}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": ComponentFloat, "count": 3, "type": "VEC3"},
			{"bufferView": 1, "componentType": ComponentUnsignedByte, "normalized": true, "count": 3, "type": "VEC4"},
			{"bufferView": 2, "componentType": ComponentUnsignedByte, "count": 3, "type": "SCALAR"},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": 0, "byteLength": colourOffset},
			{"buffer": 0, "byteOffset": colourOffset, "byteLength": indexOffset - colourOffset},
			{"buffer": 0, "byteOffset": indexOffset, "byteLength": 3},
			{"buffer": 0, "byteOffset": unusedOffset, "byteLength": 64},
		},
		"buffers": []map[string]any{{"byteLength": bin.Len()}},
	}
	return buildGLB(t, doc, bin.Bytes())
}

func TestParseReadsAccessors(t *testing.T) {
	document, err := Parse(triangleGLB(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	positions, components, err := document.ReadAccessor(0)
	if err != nil {
		t.Fatal(err)
	}
	if components != 3 || len(positions) != 9 || positions[3] != 1 {
		t.Fatalf("unexpected positions %v", positions)
	}
	colours, components, err := document.ReadAccessor(1)
	if err != nil {
		t.Fatal(err)
	}
	if components != 4 || math.Abs(colours[0]-1) > 1e-6 || colours[1] != 0 {
		t.Fatalf("normalized colours decoded as %v", colours[:4])
	}
	indices, err := document.ReadIndices(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(indices) != 3 || indices[2] != 2 {
		t.Fatalf("unexpected indices %v", indices)
	}
}

func TestWriteGLBDropsUnreachableViews(t *testing.T) {
	document, err := Parse(triangleGLB(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := document.WriteGLB()
	if err != nil {
		t.Fatal(err)
	}
	reparsed, err := Parse(out, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reparsed.BufferViews) != 3 {
		t.Fatalf("writer kept %d buffer views, want 3", len(reparsed.BufferViews))
	}
	if len(reparsed.Buffers) != 1 {
		t.Fatalf("writer produced %d buffers, want 1", len(reparsed.Buffers))
	}
	positions, _, err := reparsed.ReadAccessor(0)
	if err != nil {
		t.Fatal(err)
	}
	if positions[3] != 1 || positions[7] != 1 {
		t.Fatalf("positions survived as %v", positions)
	}
}

func TestSetAccessorDataKeepsComponentType(t *testing.T) {
	document, err := Parse(triangleGLB(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Halve the colours and write them back as normalized bytes.
	colours, components, err := document.ReadAccessor(1)
	if err != nil {
		t.Fatal(err)
	}
	for i := range colours {
		colours[i] *= 0.5
	}
	if err := document.SetAccessorData(1, colours, "VEC4", ComponentUnsignedByte, true, TargetArrayBuffer); err != nil {
		t.Fatal(err)
	}
	out, err := document.WriteGLB()
	if err != nil {
		t.Fatal(err)
	}
	reparsed, err := Parse(out, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reparsed.Accessors[1].ComponentType != ComponentUnsignedByte || !reparsed.Accessors[1].Normalized {
		t.Fatalf("accessor lost its component type: %+v", reparsed.Accessors[1])
	}
	rewritten, _, err := reparsed.ReadAccessor(1)
	if err != nil {
		t.Fatal(err)
	}
	if components != 4 || math.Abs(rewritten[0]-0.5) > 0.01 {
		t.Fatalf("rewritten colours %v", rewritten[:4])
	}
	if len(reparsed.Accessors[1].Min) != 4 {
		t.Fatalf("writer did not record accessor bounds: %+v", reparsed.Accessors[1].Min)
	}
}

func TestWriteGLBKeepsViewsWhenExtensionsAreUnknown(t *testing.T) {
	document, err := Parse(triangleGLB(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	document.ExtensionsUsed = []string{"EXT_meshopt_compression"}
	out, err := document.WriteGLB()
	if err != nil {
		t.Fatal(err)
	}
	reparsed, err := Parse(out, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reparsed.BufferViews) != 4 {
		t.Fatalf("writer kept %d views, want all 4 when an extension may hold a reference", len(reparsed.BufferViews))
	}
}

func TestParseRejectsUnknownDocument(t *testing.T) {
	if _, err := Parse([]byte("not a gltf"), nil); err == nil {
		t.Fatal("expected a format error")
	}
}

func TestAddAccessorAppends(t *testing.T) {
	document, err := Parse(triangleGLB(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	index, err := document.AddAccessor([]float64{0, 0, 1, 0, 0}, "SCALAR", ComponentFloat, false, TargetArrayBuffer)
	if err != nil {
		t.Fatal(err)
	}
	if index != 3 {
		t.Fatalf("new accessor index %d, want 3", index)
	}
	values, _, err := document.ReadAccessor(index)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 5 || values[2] != 1 {
		t.Fatalf("unexpected values %v", values)
	}
}
