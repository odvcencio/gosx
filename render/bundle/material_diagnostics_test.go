//go:build !js || !wasm

package bundle

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
)

func TestRendererReportsAndResetsCustomMaterialFallback(t *testing.T) {
	r, err := New(Config{Device: newFakeDevice(), Surface: fakeSurface{}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	b := engine.RenderBundle{Materials: []engine.RenderMaterial{{Kind: "custom", ShaderLayout: map[string]any{
		"programs": map[string]any{"metal": engine.ShaderProgram{Source: "authored metal"}},
	}}}}
	if notes := MaterialDiagnostics(b); len(notes) != 0 {
		t.Fatal("unused material reported as substituted")
	}
	b.InstancedMeshes = []engine.RenderInstancedMesh{{
		Kind: "box", Width: 1, Height: 1, Depth: 1, InstanceCount: 1,
		Transforms: []float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1},
	}}
	if err := r.Frame(b, 32, 32, 0); err != nil {
		t.Fatal(err)
	}
	stats := r.Stats()
	if len(stats.MaterialFallbacks) != 1 || stats.MaterialFallbacks[0].Code != "scene.native.custom_material_fallback" {
		t.Fatalf("missing truth: %+v", stats)
	}
	stats.MaterialFallbacks[0].Code = "changed"
	if r.Stats().MaterialFallbacks[0].Code == "changed" {
		t.Fatal("stats leaked mutable diagnostics")
	}
	if err := r.Frame(engine.RenderBundle{}, 32, 32, 0); err != nil {
		t.Fatal(err)
	}
	if len(r.Stats().MaterialFallbacks) != 0 {
		t.Fatal("fallback truth survived unrelated frame")
	}
}

func TestMaterialDiagnosticsTracksMutableBundle(t *testing.T) {
	programs := map[string]any{"metal": engine.ShaderProgram{Source: "metal source"}}
	b := engine.RenderBundle{
		Materials: []engine.RenderMaterial{
			{Kind: "custom", ShaderLayout: map[string]any{"programs": programs}},
			{Kind: "standard", ShaderBackend: "selena"},
			{Kind: "custom"},
			{Kind: "standard"},
		},
		MeshObjects: []engine.RenderObject{
			{MaterialIndex: 0, VertexCount: 3},
			{MaterialIndex: -1, VertexCount: 3},
			{MaterialIndex: 4, VertexCount: 3},
			{MaterialIndex: 3, VertexCount: 3},
		},
		InstancedMeshes: []engine.RenderInstancedMesh{{MaterialIndex: 1, InstanceCount: 1, Transforms: make([]float64, 16)}},
		Surfaces:        []engine.RenderSurface{{MaterialIndex: 2, VertexCount: 6}},
	}
	var cache materialDiagnosticsCache
	assert := func(indices []int, targets []string) {
		t.Helper()
		got := cache.update(b)
		if len(got) != len(indices) {
			t.Fatalf("diagnostics: %+v, want material indices %v", got, indices)
		}
		for i, index := range indices {
			if !strings.HasPrefix(got[i].Message, fmt.Sprintf("material %d:", index)) || !strings.HasSuffix(got[i].Message, "retained targets: ["+targets[i]+"])") {
				t.Fatalf("material %d: %+v, want targets %q", index, got[i], targets[i])
			}
		}
	}
	assert([]int{0, 1, 2}, []string{"metal", "", ""})

	// The same bundle, slices and maps change without any generation counter.
	// Removing one stage makes a retained program incomplete immediately.
	programs["metal"] = engine.ShaderProgram{}
	programs["gles"] = map[string]any{"vertex": "v", "fragment": "f"}
	assert([]int{0, 1, 2}, []string{"gles", "", ""})
	programs["gles"].(map[string]any)["fragment"] = ""
	assert([]int{0, 1, 2}, []string{"", "", ""})

	b.MeshObjects[0].VertexCount = 0
	b.InstancedMeshes[0].Transforms = nil
	b.Surfaces[0].VertexCount = 0
	assert(nil, nil)
	b.MeshObjects[0].VertexCount = 3
	b.MeshObjects[0].MaterialIndex = 2
	assert([]int{2}, []string{""})
	b.Materials[2].Kind = "standard"
	assert(nil, nil)
	b.Materials[2].ShaderBackend = "selena"
	assert([]int{2}, []string{""})

	// Shrinking and regrowing the material table cannot preserve old membership.
	b.Materials = b.Materials[:1]
	assert(nil, nil)
	b.Materials = b.Materials[:3]
	assert([]int{2}, []string{""})
	b.MeshObjects[0].MaterialIndex = 0
	b.InstancedMeshes[0].Transforms = make([]float64, 16)
	b.InstancedMeshes[0].InstanceCount = 0
	assert([]int{0}, []string{""})
}

func TestMaterialFallbackStatsOwnReusedDiagnostics(t *testing.T) {
	b := diagnosticScaleBundle(2)
	var cache materialDiagnosticsCache
	var stats frameStatsRecorder
	stats.setMaterialFallbacks(cache.update(b))
	before := stats.snapshot()
	b.MeshObjects[0].VertexCount = 0
	cache.update(b)
	if got := stats.snapshot(); !reflect.DeepEqual(got.MaterialFallbacks, before.MaterialFallbacks) {
		t.Fatal("cache mutation changed the published snapshot")
	}
	stats.setMaterialFallbacks(cache.update(b))
	if len(before.MaterialFallbacks) != 2 || !strings.HasPrefix(before.MaterialFallbacks[0].Message, "material 0:") {
		t.Fatal("publishing a new frame changed a caller's prior snapshot")
	}
}

func TestMaterialDiagnosticsSteadyFrameAllocatesNothing(t *testing.T) {
	b := diagnosticScaleBundle(100)
	var cache materialDiagnosticsCache
	var stats frameStatsRecorder
	stats.setMaterialFallbacks(cache.update(b))
	if allocs := testing.AllocsPerRun(100, func() {
		stats.setMaterialFallbacks(cache.update(b))
	}); allocs != 0 {
		t.Fatalf("steady diagnostics allocate %g times per frame, want zero", allocs)
	}
}
