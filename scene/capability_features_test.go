package scene

import (
	"os"
	"strings"
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

func runtimeFeatureCollectionWired(t *testing.T, marker string) bool {
	t.Helper()
	source, err := os.ReadFile("scene_ir.go")
	if err != nil {
		t.Fatalf("read scene_ir.go: %v", err)
	}
	return strings.Contains(string(source), marker)
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

	t.Run("water system triggers water simulation", func(t *testing.T) {
		ir := SceneIR{
			WaterSystems: []WaterSystemIR{{ID: "pool-water", Resolution: 256}},
		}
		got := featureSet(collectFeatures(ir))
		if !got[capability.FeatureWaterSim] {
			t.Error("expected FeatureWaterSim from waterSystems; not present")
		}
		if got[capability.FeatureWaterObjectTexturePass] {
			t.Error("did not expect FeatureWaterObjectTexturePass without an active water object")
		}
		if got[capability.FeatureWaterObjectMeshShadowPass] {
			t.Error("did not expect FeatureWaterObjectMeshShadowPass without a mesh-projected water object")
		}
	})

	t.Run("declared water object target triggers object texture pass capability", func(t *testing.T) {
		ir := SceneIR{
			WaterSystems: []WaterSystemIR{{
				ID:                      "pool-water",
				Resolution:              256,
				ActiveObject:            "float-sphere",
				ObjectKind:              "Sphere",
				ObjectTextureResolution: 512,
			}},
		}
		features := collectFeatures(ir)
		got := featureSet(features)
		if !got[capability.FeatureWaterSim] {
			t.Error("expected FeatureWaterSim from waterSystems; not present")
		}
		if !got[capability.FeatureWaterObjectTexturePass] {
			t.Error("expected FeatureWaterObjectTexturePass from declared object texture target; not present")
		}
		if got[capability.FeatureWaterObjectMeshShadowPass] {
			t.Error("did not expect FeatureWaterObjectMeshShadowPass from an analytic object texture target")
		}
		if len(features) < 2 || features[0] != capability.FeatureWaterObjectTexturePass || features[1] != capability.FeatureWaterSim {
			t.Fatalf("expected deterministic water feature order [water-object-texture-pass water-simulation], got %v", features)
		}
	})

	t.Run("complex water object triggers object texture pass capability", func(t *testing.T) {
		ir := SceneIR{
			WaterSystems: []WaterSystemIR{{
				ID:           "pool-water",
				Resolution:   256,
				ActiveObject: "TorusKnot",
				ObjectKind:   "compound",
			}},
		}
		got := featureSet(collectFeatures(ir))
		if !got[capability.FeatureWaterObjectTexturePass] {
			t.Error("expected FeatureWaterObjectTexturePass from complex water object; not present")
		}
		if !got[capability.FeatureWaterObjectMeshShadowPass] {
			t.Error("expected FeatureWaterObjectMeshShadowPass from complex water object; not present")
		}
	})

	t.Run("authored mesh shadow shader triggers mesh shadow pass capability", func(t *testing.T) {
		ir := SceneIR{
			WaterSystems: []WaterSystemIR{{
				ID:                           "pool-water",
				Resolution:                   256,
				ObjectMeshShadowVertexWGSL:   "@vertex fn vertexMain() -> @builtin(position) vec4f { return vec4f(); }",
				ObjectMeshShadowFragmentWGSL: "@fragment fn fragmentMain() -> @location(0) vec4f { return vec4f(); }",
			}},
		}
		got := featureSet(collectFeatures(ir))
		if !got[capability.FeatureWaterObjectMeshShadowPass] {
			t.Error("expected FeatureWaterObjectMeshShadowPass from authored mesh shadow shader; not present")
		}
	})

	t.Run("compute particles system triggers compute-particles capability", func(t *testing.T) {
		ir := SceneIR{
			ComputeParticles: []ComputeParticlesIR{{ID: "dust", Count: 100}},
		}
		got := featureSet(collectFeatures(ir))
		want := runtimeFeatureCollectionWired(t, "seen[capability.FeatureComputeParts] = true")
		if got[capability.FeatureComputeParts] != want {
			t.Errorf("FeatureComputeParts present=%v, want runtime collector phase %v", got[capability.FeatureComputeParts], want)
		}
	})

	t.Run("no compute particles: no compute-particles capability", func(t *testing.T) {
		ir := SceneIR{
			Objects: []ObjectIR{{ID: "box", Kind: "box"}},
		}
		got := featureSet(collectFeatures(ir))
		if got[capability.FeatureComputeParts] {
			t.Error("did not expect FeatureComputeParts without computeParticles")
		}
	})

	t.Run("instanced mesh with cull kernel triggers gpu-cull capability", func(t *testing.T) {
		ir := SceneIR{
			InstancedMeshes: []InstancedMeshIR{{
				ID:             "rocks",
				Count:          200,
				CullKernelWGSL: "@compute @workgroup_size(64) fn cull() {}",
			}},
		}
		got := featureSet(collectFeatures(ir))
		want := runtimeFeatureCollectionWired(t, "seen[capability.FeatureGPUCull] = true")
		if got[capability.FeatureGPUCull] != want {
			t.Errorf("FeatureGPUCull present=%v, want runtime collector phase %v", got[capability.FeatureGPUCull], want)
		}
	})

	t.Run("instanced mesh with hoisted cull kernel ref triggers gpu-cull capability", func(t *testing.T) {
		ir := SceneIR{
			InstancedMeshes: []InstancedMeshIR{{
				ID:                "rocks",
				Count:             200,
				CullKernelWGSLRef: "sl:abc123",
			}},
		}
		got := featureSet(collectFeatures(ir))
		want := runtimeFeatureCollectionWired(t, "seen[capability.FeatureGPUCull] = true")
		if got[capability.FeatureGPUCull] != want {
			t.Errorf("FeatureGPUCull present=%v, want runtime collector phase %v", got[capability.FeatureGPUCull], want)
		}
	})

	t.Run("instanced mesh without cull kernel: no gpu-cull capability", func(t *testing.T) {
		ir := SceneIR{
			InstancedMeshes: []InstancedMeshIR{{ID: "rocks", Count: 200}},
		}
		got := featureSet(collectFeatures(ir))
		if got[capability.FeatureGPUCull] {
			t.Error("did not expect FeatureGPUCull without a cull kernel")
		}
	})

	t.Run("explicit wireframe object triggers wireframe capability", func(t *testing.T) {
		ir := SceneIR{
			Objects: []ObjectIR{{ID: "cage", Kind: "box", Wireframe: Bool(true)}},
		}
		got := featureSet(collectFeatures(ir))
		if !got[capability.FeatureWireframe] {
			t.Error("expected FeatureWireframe from an explicit authored Wireframe; not present")
		}
	})

	t.Run("explicit wireframe instanced mesh triggers wireframe capability", func(t *testing.T) {
		ir := SceneIR{
			InstancedMeshes: []InstancedMeshIR{{ID: "cages", Count: 4, Wireframe: Bool(true)}},
		}
		got := featureSet(collectFeatures(ir))
		if !got[capability.FeatureWireframe] {
			t.Error("expected FeatureWireframe from an explicit authored InstancedMeshIR.Wireframe; not present")
		}
	})

	t.Run("explicit wireframe false does not trigger wireframe capability", func(t *testing.T) {
		ir := SceneIR{
			Objects: []ObjectIR{{ID: "solid", Kind: "box", Wireframe: Bool(false)}},
		}
		got := featureSet(collectFeatures(ir))
		if got[capability.FeatureWireframe] {
			t.Error("did not expect FeatureWireframe from an explicit Wireframe:false")
		}
	})

	t.Run("unauthored wireframe does not trigger wireframe capability", func(t *testing.T) {
		ir := SceneIR{
			Objects: []ObjectIR{{ID: "plain", Kind: "box"}},
		}
		got := featureSet(collectFeatures(ir))
		if got[capability.FeatureWireframe] {
			t.Error("did not expect FeatureWireframe when the wire record never sets Wireframe (nil)")
		}
	})
}

// TestCollectFeaturesRaisesWireframeOnlyWhenAuthored is the non-regression
// case the cluster spec names as risk R4: collectFeatures must raise
// wireframe for an EXPLICIT authored Wireframe only, never for a legacy
// default. The subtests inside TestCollectFeatures above already cover the
// explicit-true, explicit-false and unauthored (nil) cases against
// collectFeatures directly.
//
// This test covers the other half: the scene package's own author-facing
// lowering (Props.SceneIR, through the StandardMaterial.Wireframe field) must
// leave Wireframe nil for a mesh that never authors it, so an untextured
// "flat" mesh — which the legacy client/vm pipeline routes to a world-line
// pass by a DIFFERENT default it owns — carries no wireframe claim on this
// wire format at all. There is no default here to trip; this test is the
// record that stays true only as long as that remains so.
func TestCollectFeaturesRaisesWireframeOnlyWhenAuthored(t *testing.T) {
	props := Props{
		Graph: Graph{
			Nodes: []Node{
				Mesh{
					ID:       "untextured-flat",
					Geometry: CubeGeometry{Size: 1},
					// No Material at all: the most natural authoring of an
					// untextured "flat" mesh, and the shape client/vm's
					// separate default-true routing keys off in its own
					// pipeline (which this wire format never reaches).
				},
				Mesh{
					ID:       "explicit-wireframe",
					Geometry: CubeGeometry{Size: 1},
					Material: StandardMaterial{Wireframe: Bool(true)},
				},
			},
		},
	}
	ir := props.SceneIR()

	var flat, wireframeObj *ObjectIR
	for i := range ir.Objects {
		switch ir.Objects[i].ID {
		case "untextured-flat":
			flat = &ir.Objects[i]
		case "explicit-wireframe":
			wireframeObj = &ir.Objects[i]
		}
	}
	if flat == nil || wireframeObj == nil {
		t.Fatalf("expected both objects in the lowered IR; got %+v", ir.Objects)
	}
	if flat.Wireframe != nil {
		t.Errorf("an untextured flat mesh that never authors wireframe must lower Wireframe to nil, got %v", *flat.Wireframe)
	}
	if wireframeObj.Wireframe == nil || !*wireframeObj.Wireframe {
		t.Fatalf("an explicit StandardMaterial{Wireframe: Bool(true)} must lower Wireframe to true, got %v", wireframeObj.Wireframe)
	}

	got := featureSet(collectFeatures(ir))
	if !got[capability.FeatureWireframe] {
		t.Error("expected FeatureWireframe from the explicit-wireframe mesh; not present")
	}
}

// TestComputeParticlesReportsWebGLDegraded verifies the honesty-gate fix:
// a scene using ComputeParticles must report WebGL as degraded (present in
// Capable, listed under Degraded[webgl]) instead of silently claiming full
// WebGL support while the runtime falls back to a CPU particle simulation.
func TestComputeParticlesReportsWebGLDegraded(t *testing.T) {
	props := Props{
		Graph: NewGraph(ComputeParticles{
			ID:    "dust",
			Count: 64,
			Emitter: ParticleEmitter{
				Kind:   "sphere",
				Radius: 1,
			},
			Material: ParticleMaterial{Color: "#ffffff"},
		}),
	}
	ir := props.SceneIR()

	if ir.BackendCaps == nil {
		t.Fatal("BackendCaps is nil")
	}

	// compute-particles is not in DefaultPolicy().Required, so WebGL stays
	// Capable — but it must show up as degraded, not silently full-featured.
	capableWebGL := false
	for _, b := range ir.BackendCaps.Capable {
		if b == capability.BackendWebGL {
			capableWebGL = true
			break
		}
	}
	if !capableWebGL {
		t.Fatalf("expected WebGL to remain Capable (compute-particles is optional); got %v", ir.BackendCaps.Capable)
	}

	degraded := ir.BackendCaps.Degraded[capability.BackendWebGL]
	found := false
	for _, f := range degraded {
		if f == capability.FeatureComputeParts {
			found = true
			break
		}
	}
	want := runtimeFeatureCollectionWired(t, "seen[capability.FeatureComputeParts] = true")
	if found != want {
		t.Fatalf("FeatureComputeParts degraded=%v, want runtime collector phase %v; got %v", found, want, degraded)
	}
}

// TestSkinLookupDetectsSkinning verifies that collectFeatures tags
// FeatureSkinning when the injected SkinLookup returns true for a Model src.
func TestSkinLookupDetectsSkinning(t *testing.T) {
	const skinnedURL = "/models/soldier.glb"

	t.Run("with lookup: Model src skinned → FeatureSkinning + webgpu/webgl", func(t *testing.T) {
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

		// BackendCaps: skinning is required, and both WebGPU and WebGL can render it.
		if ir.BackendCaps == nil {
			t.Fatal("BackendCaps is nil")
		}
		wantCapable := map[capability.Backend]bool{
			capability.BackendWebGPU: true,
			capability.BackendWebGL:  true,
		}
		if len(ir.BackendCaps.Capable) != len(wantCapable) {
			t.Fatalf("expected Capable=[webgpu webgl]; got %v", ir.BackendCaps.Capable)
		}
		for _, backend := range ir.BackendCaps.Capable {
			if !wantCapable[backend] {
				t.Errorf("unexpected capable backend %q; got %v", backend, ir.BackendCaps.Capable)
			}
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
