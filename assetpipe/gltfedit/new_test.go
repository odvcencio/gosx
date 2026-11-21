package gltfedit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewDocumentWritesGLBAndSplitGLTF(t *testing.T) {
	d := New("unit test")
	pos, err := d.AddAccessor([]float64{0, 0, 0, 1, 0, 0, 0, 1, 0}, "VEC3", ComponentFloat, false, TargetArrayBuffer)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := d.AddAccessorInts([]int32{0, 1, 2}, "SCALAR", ComponentUnsignedShort, false, TargetElementArrayBuffer)
	if err != nil {
		t.Fatal(err)
	}
	mat := 0
	d.Meshes = []Mesh{{Primitives: []Primitive{{Attributes: map[string]int{"POSITION": pos}, Indices: &idx, Material: &mat}}}}
	zero := 0
	d.AddNode(Node{Name: "tri", Mesh: &zero})
	d.Scenes = []Scene{{Nodes: []int{0}}}
	d.MarkScenesChanged()
	if err := d.SetSection("materials", []map[string]any{{"name": "m"}}); err != nil {
		t.Fatal(err)
	}
	if err := d.SetSection("nodes", nil); err == nil {
		t.Fatal("SetSection accepted a typed section")
	}
	view := d.AddBufferView([]byte("fake png"), 0)
	if err := d.SetSection("images", []map[string]any{{"bufferView": view, "mimeType": "image/png"}}); err != nil {
		t.Fatal(err)
	}

	glb, err := d.WriteGLB()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(glb, nil)
	if err != nil {
		t.Fatal(err)
	}
	var asset map[string]string
	if ok, err := back.Section("asset", &asset); !ok || err != nil || asset["generator"] != "unit test" || asset["version"] != "2.0" {
		t.Fatalf("asset %v %v %v", asset, ok, err)
	}
	if got, _, err := back.ReadAccessor(pos); err != nil || len(got) != 9 || got[3] != 1 {
		t.Fatalf("positions %v %v", got, err)
	}

	// The split form packs the same bytes behind a URI; a second document
	// differing only in its image shares the buffer.
	if err := d.SetSection("images", []map[string]any{{"uri": "atlas-a.png"}}); err != nil {
		t.Fatal(err)
	}
	js1, bin1, err := d.WriteGLTF("shared.bin")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetSection("images", []map[string]any{{"uri": "atlas-b.png"}}); err != nil {
		t.Fatal(err)
	}
	_, bin2, err := d.WriteGLTF("shared.bin")
	if err != nil {
		t.Fatal(err)
	}
	if string(bin1) != string(bin2) || strings.Contains(string(bin1), "fake png") {
		t.Fatal("split buffers differ or still hold the unreferenced image")
	}
	var root struct {
		Buffers []Buffer `json:"buffers"`
	}
	if err := json.Unmarshal(js1, &root); err != nil || len(root.Buffers) != 1 || root.Buffers[0].URI != "shared.bin" || root.Buffers[0].ByteLength != len(bin1) {
		t.Fatalf("buffers %+v %v", root.Buffers, err)
	}
	split, err := Parse(js1, func(uri string) ([]byte, error) { return bin1, nil })
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := split.ReadAccessor(pos); err != nil || got[7] != 1 {
		t.Fatalf("split positions %v %v", got, err)
	}
	if _, _, err := d.WriteGLTF(""); err == nil {
		t.Fatal("WriteGLTF accepted an empty URI")
	}
}
