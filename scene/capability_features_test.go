package scene

import (
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

// mockSkinLookup is a test helper that returns true for URLs in the set.
type mockSkinLookup struct{ skinned map[string]bool }

func (m *mockSkinLookup) Skinned(src string) bool { return m.skinned[src] }

func featureSet(features []capability.Feature) map[capability.Feature]bool {
	set := make(map[capability.Feature]bool, len(features))
	for _, f := range features {
		set[f] = true
	}
	return set
}

// TestCollectFeatures verifies that collectFeatures correctly detects ibl,
// gpu-picking, and line-dashed features from the wire SceneIR.
func TestCollectFeatures(t *testing.T) {
	t.Run("detects all three features", func(t *testing.T) {
		pickable := true
		props := Props{
			Environment: Environment{
				EnvironmentMap: "env.hdr",
			},
			Graph: Graph{
				Nodes: []Node{
					Mesh{
						ID:       "pickable-box",
						Geometry: CubeGeometry{Size: 1},
						Pickable: &pickable,
					},
					Mesh{
						ID:       "dashed-line",
						Geometry: LinesGeometry{Points: []Vector3{{}, {X: 1}}, Segments: [][2]int{{0, 1}}},
						Material: LineDashedMaterial{DashSize: 0.1, GapSize: 0.1},
					},
				},
			},
		}
		ir := props.SceneIR()
		got := featureSet(collectFeatures(ir))

		if !got[capability.FeatureIBL] {
			t.Error("expected FeatureIBL; not present")
		}
		if !got[capability.FeatureGPUPicking] {
			t.Error("expected FeatureGPUPicking; not present")
		}
		if !got[capability.FeatureLineDashed] {
			t.Error("expected FeatureLineDashed; not present")
		}
	})

	t.Run("plain scene returns no features", func(t *testing.T) {
		props := Props{
			Graph: Graph{
				Nodes: []Node{
					Mesh{
						ID:       "plain-box",
						Geometry: CubeGeometry{Size: 1},
					},
				},
			},
		}
		ir := props.SceneIR()
		got := collectFeatures(ir)
		if len(got) != 0 {
			t.Errorf("expected no features; got %v", got)
		}
	})

	t.Run("instanced GLB pickable triggers gpu-picking", func(t *testing.T) {
		pickable := true
		ir := SceneIR{
			InstancedGLBMeshes: []InstancedGLBMeshIR{
				{ID: "batch-1", Src: "model.glb", Pickable: &pickable},
			},
		}
		got := featureSet(collectFeatures(ir))
		if !got[capability.FeatureGPUPicking] {
			t.Error("expected FeatureGPUPicking from instanced GLB; not present")
		}
	})

	t.Run("no duplicates when multiple objects share a feature", func(t *testing.T) {
		pickable := true
		ir := SceneIR{
			Objects: []ObjectIR{
				{ID: "a", Kind: "box", Pickable: &pickable},
				{ID: "b", Kind: "box", Pickable: &pickable},
			},
		}
		got := collectFeatures(ir)
		count := 0
		for _, f := range got {
			if f == capability.FeatureGPUPicking {
				count++
			}
		}
		if count != 1 {
			t.Errorf("expected FeatureGPUPicking exactly once; got %d", count)
		}
	})
}

// TestSkinLookupDetectsSkinning verifies that collectFeatures tags
// FeatureSkinning when the injected SkinLookup returns true for a Model src.
func TestSkinLookupDetectsSkinning(t *testing.T) {
	const skinnedURL = "/models/soldier.glb"

	t.Run("with lookup: Model src skinned → FeatureSkinning + webgl-only", func(t *testing.T) {
		lookup := &mockSkinLookup{skinned: map[string]bool{skinnedURL: true}}
		SetSkinLookup(lookup)
		t.Cleanup(func() { SetSkinLookup(nil) })

		props := Props{
			Graph: Graph{
				Nodes: []Node{
					Model{ID: "hero", Src: skinnedURL},
				},
			},
		}
		ir := props.SceneIR()

		// collectFeatures should include FeatureSkinning.
		got := featureSet(collectFeatures(ir))
		if !got[capability.FeatureSkinning] {
			t.Error("expected FeatureSkinning; not present")
		}

		// BackendCaps: skinning is required → only webgl in Capable.
		if ir.BackendCaps == nil {
			t.Fatal("BackendCaps is nil")
		}
		if len(ir.BackendCaps.Capable) != 1 || ir.BackendCaps.Capable[0] != capability.BackendWebGL {
			t.Errorf("expected Capable=[webgl]; got %v", ir.BackendCaps.Capable)
		}
	})

	t.Run("with lookup: InstancedGLBMesh src skinned → FeatureSkinning", func(t *testing.T) {
		lookup := &mockSkinLookup{skinned: map[string]bool{skinnedURL: true}}
		SetSkinLookup(lookup)
		t.Cleanup(func() { SetSkinLookup(nil) })

		ir := SceneIR{
			InstancedGLBMeshes: []InstancedGLBMeshIR{
				{ID: "batch-1", Src: skinnedURL},
			},
		}
		got := featureSet(collectFeatures(ir))
		if !got[capability.FeatureSkinning] {
			t.Error("expected FeatureSkinning from InstancedGLBMesh; not present")
		}
	})

	t.Run("no lookup (nil): same scene tags no skinning, Capable stays three", func(t *testing.T) {
		SetSkinLookup(nil)

		props := Props{
			Graph: Graph{
				Nodes: []Node{
					Model{ID: "hero", Src: skinnedURL},
				},
			},
		}
		ir := props.SceneIR()

		got := featureSet(collectFeatures(ir))
		if got[capability.FeatureSkinning] {
			t.Error("expected no FeatureSkinning when lookup is nil")
		}

		if ir.BackendCaps == nil {
			t.Fatal("BackendCaps is nil")
		}
		// No constrained features → all three backends capable.
		if len(ir.BackendCaps.Capable) != 3 {
			t.Errorf("expected Capable=[webgpu,webgl,canvas2d]; got %v", ir.BackendCaps.Capable)
		}
	})
}
