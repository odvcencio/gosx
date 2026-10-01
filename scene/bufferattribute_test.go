package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func quadGeometry() BufferGeometry {
	return BufferGeometry{
		// Two-triangle quad over four unique vertices.
		Positions: []float64{
			0, 0, 0,
			1, 0, 0,
			1, 1, 0,
			0, 1, 0,
		},
		Normals: []float64{
			0, 0, 1,
			0, 0, 1,
			0, 0, 1,
			0, 0, 1,
		},
		UVs: []float64{
			0, 0,
			1, 0,
			1, 1,
			0, 1,
		},
		Indices:   []int{0, 1, 2, 0, 2, 3},
		Immutable: true,
		Revision:  3,
	}
}

func TestValidBufferAttributeName(t *testing.T) {
	valid := []string{"a", "heat", "aHeat_2", "_private", "CUSTOM_X"}
	for _, name := range valid {
		if !ValidBufferAttributeName(name) {
			t.Errorf("ValidBufferAttributeName(%q) = false, want true", name)
		}
	}
	invalid := []string{
		"",           // empty
		"2hot",       // leading digit
		"has space",  // not an identifier
		"heat-value", // dash is not a shader identifier character
		"heat.value", // dot likewise
		"positions",  // built-in collision
		"position",   // built-in collision
		"normals",    // built-in collision
		"normal",     // built-in collision
		"uvs",        // built-in collision
		"uv",         // built-in collision
		"tangents",   // built-in collision
		"tangent",    // built-in collision
		"indices",    // built-in collision
		"index",      // built-in collision
		"joints",     // skin stream collision
		"weights",    // skin stream collision
	}
	for _, name := range invalid {
		if ValidBufferAttributeName(name) {
			t.Errorf("ValidBufferAttributeName(%q) = true, want false", name)
		}
	}
}

func TestBufferGeometryCustomAttributesLowerAndMarshal(t *testing.T) {
	g := quadGeometry()
	g.Attributes = map[string]BufferAttribute{
		"heat":    {Data: []float64{0, 0.25, 0.5, 1}, ItemSize: 1},
		"flowDir": {Data: []float64{1, 0, 0, 1, 0, -1, -1, 0}, ItemSize: 2},
		"tint": {Data: []float64{
			1, 0, 0, 1,
			0, 1, 0, 1,
			0, 0, 1, 1,
			1, 1, 0, 1,
		}, ItemSize: 4},
		"wobble": {Data: []float64{0.5, 0.25, 0.125, 0.0625}, ItemSize: 1},
	}
	v := bufferGeometryVertices(g)
	if v == nil {
		t.Fatal("bufferGeometryVertices returned nil for valid immutable custom geometry")
	}
	if v.Count != 4 {
		t.Fatalf("Count = %d, want 4", v.Count)
	}
	wantSizes := map[string]int{"heat": 1, "flowDir": 2, "wobble": 1, "tint": 4}
	if len(v.Attributes) != len(wantSizes) {
		t.Fatalf("len(Attributes) = %d, want %d", len(v.Attributes), len(wantSizes))
	}
	for name, itemSize := range wantSizes {
		attr, ok := v.Attributes[name]
		if !ok {
			t.Fatalf("missing attribute %q", name)
		}
		if attr.ItemSize != itemSize {
			t.Errorf("%q ItemSize = %d, want %d", name, attr.ItemSize, itemSize)
		}
		if len(attr.Data) != v.Count*itemSize {
			t.Errorf("%q len(Data) = %d, want %d", name, len(attr.Data), v.Count*itemSize)
		}
	}
	if got := v.Attributes["flowDir"].Data; got[2] != 0 || got[5] != -1 {
		t.Errorf("flowDir values changed through lowering: %v", got)
	}

	wire, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var back struct {
		Attributes map[string]struct {
			Data     []float64 `json:"data"`
			ItemSize int       `json:"itemSize"`
		} `json:"attributes"`
		Indices []uint32 `json:"indices"`
	}
	if err := json.Unmarshal(wire, &back); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(back.Attributes) != 4 {
		t.Fatalf("wire attribute count = %d, want 4", len(back.Attributes))
	}
	for _, name := range []string{"heat", "vec2stream"} {
		_ = name
	}
	if back.Attributes["heat"].ItemSize != 1 || len(back.Attributes["heat"].Data) != 4 {
		t.Errorf("scalar wire stream wrong: %+v", back.Attributes["heat"])
	}
	if back.Attributes["flowDir"].ItemSize != 2 || !strings.Contains(string(wire), "\"itemSize\":2") {
		t.Errorf("vec2 wire stream wrong: %+v", back.Attributes["flowDir"])
	}
	if back.Attributes["tint"].ItemSize != 4 || len(back.Attributes["tint"].Data) != 16 {
		t.Errorf("vec4 wire stream wrong: %+v", back.Attributes["tint"])
	}
	if back.Attributes["wobble"].Data[3] != 0.0625 {
		t.Errorf("exact scalar value lost: %+v", back.Attributes["wobble"])
	}
	if len(back.Indices) != 6 || back.Indices[4] != 2 {
		t.Errorf("indexed geometry must stay indexed: %v", back.Indices)
	}
}

// TestBufferGeometryCustomAttributesVec3 covers the vec3 tuple width end to
// end: lowering preserves ItemSize 3 and Count*3 values, and the wire shape
// keeps "itemSize":3 so a JS consumer binds an exact vec3 fetch.
func TestBufferGeometryCustomAttributesVec3(t *testing.T) {
	g := quadGeometry()
	g.Attributes = map[string]BufferAttribute{
		"offsetVec": {Data: []float64{
			1, 2, 3,
			4, 5, 6,
			7, 8, 9,
			10, 11, 12,
		}, ItemSize: 3},
	}
	v := bufferGeometryVertices(g)
	if v == nil {
		t.Fatal("bufferGeometryVertices returned nil for valid vec3 custom geometry")
	}
	attr, ok := v.Attributes["offsetVec"]
	if !ok {
		t.Fatal("missing vec3 attribute \"offsetVec\" after lowering")
	}
	if attr.ItemSize != 3 {
		t.Fatalf("ItemSize = %d, want 3", attr.ItemSize)
	}
	if len(attr.Data) != v.Count*3 {
		t.Fatalf("len(Data) = %d, want %d", len(attr.Data), v.Count*3)
	}
	for i, want := range []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12} {
		if attr.Data[i] != want {
			t.Fatalf("Data[%d] = %v, want %v", i, attr.Data[i], want)
		}
	}

	wire, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(wire), "\"itemSize\":3") {
		t.Errorf("wire output missing \"itemSize\":3 for vec3 stream: %s", wire)
	}
	var back struct {
		Attributes map[string]struct {
			Data     []float64 `json:"data"`
			ItemSize int       `json:"itemSize"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(wire, &back); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	got := back.Attributes["offsetVec"]
	if got.ItemSize != 3 || len(got.Data) != 12 {
		t.Errorf("vec3 wire stream wrong: %+v", got)
	}
}

func TestBufferGeometryCustomAttributesDoNotAliasCaller(t *testing.T) {
	g := quadGeometry()
	data := []float64{1, 2, 3, 4}
	g.Attributes = map[string]BufferAttribute{
		"heat": {Data: data, ItemSize: 1},
	}
	v := bufferGeometryVertices(g)
	if v == nil {
		t.Fatal("nil vertices")
	}
	// Mutate the caller's map entry and slice after lowering.
	delete(g.Attributes, "heat")
	data[0] = 99
	data[1] = math.MaxFloat64
	if got, ok := v.Attributes["heat"]; !ok {
		t.Fatal("lowered map dropped the heat entry after caller mutation")
	} else if got.Data[0] != 1 || got.Data[1] != 2 {
		t.Errorf("lowered data aliases caller slice: %v", got.Data)
	}
}

func TestBufferGeometryCustomAttributesFailClosed(t *testing.T) {
	base := quadGeometry()
	cases := map[string]func(*BufferGeometry){
		"malformed name": func(g *BufferGeometry) {
			g.Attributes = map[string]BufferAttribute{"2bad": {Data: make([]float64, 4), ItemSize: 1}}
		},
		"builtin collision": func(g *BufferGeometry) {
			g.Attributes = map[string]BufferAttribute{"uv": {Data: make([]float64, 8), ItemSize: 2}}
		},
		"item size zero": func(g *BufferGeometry) { g.Attributes = map[string]BufferAttribute{"heat": {Data: nil, ItemSize: 0}} },
		"item size too big": func(g *BufferGeometry) {
			g.Attributes = map[string]BufferAttribute{"heat": {Data: make([]float64, 20), ItemSize: 5}}
		},
		"short stream": func(g *BufferGeometry) {
			g.Attributes = map[string]BufferAttribute{"heat": {Data: []float64{1, 2, 3}, ItemSize: 1}}
		},
		"long stream": func(g *BufferGeometry) {
			g.Attributes = map[string]BufferAttribute{"heat": {Data: make([]float64, 5), ItemSize: 1}}
		},
		"non-finite NaN": func(g *BufferGeometry) {
			g.Attributes = map[string]BufferAttribute{"heat": {Data: []float64{1, 2, math.NaN(), 4}, ItemSize: 1}}
		},
		"non-finite Inf": func(g *BufferGeometry) {
			g.Attributes = map[string]BufferAttribute{"heat": {Data: []float64{1, 2, 3, math.Inf(-1)}, ItemSize: 1}}
		},
		"mutable geometry rejected": func(g *BufferGeometry) {
			g.Immutable = false
			g.Revision = 0
			g.Attributes = map[string]BufferAttribute{"heat": {Data: make([]float64, 4), ItemSize: 1}}
		},
		"dynamic geometry rejected": func(g *BufferGeometry) {
			g.Dynamic = true
			g.Attributes = map[string]BufferAttribute{"heat": {Data: make([]float64, 4), ItemSize: 1}}
		},
	}
	for name, mutate := range cases {
		g := base
		mutate(&g)
		if v := bufferGeometryVertices(g); v != nil {
			t.Errorf("%s: expected fail-closed nil vertices, got %+v", name, v)
		}
	}
}

func TestBufferGeometryWithoutCustomAttributesUnchanged(t *testing.T) {
	g := quadGeometry()
	v := bufferGeometryVertices(g)
	if v == nil {
		t.Fatal("nil vertices")
	}
	if v.Attributes != nil {
		t.Errorf("no-custom mesh serialized attributes: %+v", v.Attributes)
	}
	wire, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(wire), "attributes") {
		t.Errorf("wire output must stay byte-compatible without custom attrs: %s", wire)
	}
	if len(v.Indices) != 6 {
		t.Errorf("indices = %v, want 6 entries", v.Indices)
	}

	// Unindexed mutable geometry keeps its historical shape.
	unindexed := BufferGeometry{
		Positions: []float64{0, 0, 0, 1, 0, 0, 0, 1, 0},
		Normals:   []float64{0, 0, 1, 0, 0, 1, 0, 0, 1},
	}
	uv := bufferGeometryVertices(unindexed)
	if uv == nil || uv.Immutable || uv.Revision != nil || uv.Dynamic {
		t.Fatalf("unindexed mutable geometry shape changed: %+v", uv)
	}
	if uv.Count != 3 || len(uv.Positions) != 9 || uv.Indices != nil {
		t.Errorf("unindexed flat triangle list changed: %+v", uv)
	}
}
