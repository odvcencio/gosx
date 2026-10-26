package island

import (
	"encoding/json"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/engine"
)

func TestMaterialMotionUsesCoreKernelWithoutEngineProgram(t *testing.T) {
	for _, tc := range []struct {
		name, props string
		wantKernel  bool
	}{
		{"ordinary scene", `{"scene":{"objects":[]}}`, false},
		{"spin metadata", `{"scene":{"motionProgram":"TU9UMw=="}}`, false},
		{"nested material", `{"scene":{"materialMotionProgram":"TU9UMw=="}}`, true},
		{"flat material", `{"materialMotionProgram":"TU9UMw=="}`, true},
		{"empty", `{"scene":{"materialMotionProgram":""}}`, false},
		{"blank", `{"scene":{"materialMotionProgram":"  "}}`, false},
		{"null", `{"scene":{"materialMotionProgram":null}}`, false},
		{"wrong type", `{"scene":{"materialMotionProgram":12}}`, false},
		{"unrelated malformed field", `{"controlZoom":"bad","scene":{"materialMotionProgram":"TU9UMw=="}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRenderer("main")
			m := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{WASMVariants: map[string]buildmanifest.RuntimeVariantAsset{
				"core":   {HashedAsset: buildmanifest.HashedAsset{File: "core.wasm", Size: 650000}, Variant: "core", FeatureMask: 17},
				"engine": {HashedAsset: buildmanifest.HashedAsset{File: "engine.wasm", Size: 1700000}, Variant: "engine", FeatureMask: 27},
			}}}
			if err := r.ApplyBuildManifest(m, "/gosx/assets"); err != nil {
				t.Fatal(err)
			}
			r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(tc.props)}, gosx.Text("Scene fallback"))
			entry := r.Manifest().Engines[0]
			if got := entry.Runtime == string(engine.RuntimeKernel); got != tc.wantKernel {
				t.Fatalf("runtime=%q, want kernel=%v", entry.Runtime, tc.wantKernel)
			}
			plan := r.clientRuntimePlan()
			if plan.SharedRuntime != tc.wantKernel || plan.WASMExec != tc.wantKernel {
				t.Fatalf("wrong WASM loading plan: %+v", plan)
			}
			if r.needsSharedRuntimeEngineBridge() || r.scene3DNeedsHydrateChunk() {
				t.Fatal("material helpers pulled in engine-program hydration")
			}
			if tc.wantKernel && r.selectedRuntimeRef().Variant != "core" {
				t.Fatalf("native helpers selected %q instead of core", r.selectedRuntimeRef().Variant)
			}
			wasm := false
			for _, capability := range entry.RequiredCapabilities {
				wasm = wasm || capability == "wasm"
			}
			if wasm != tc.wantKernel {
				t.Fatal("WASM capability gate does not match the selected runtime")
			}
		})
	}
}

func TestMaterialMotionPreservesExplicitRuntimeOwnership(t *testing.T) {
	for _, mode := range []engine.Runtime{engine.RuntimeShared, engine.RuntimeGoWASM} {
		r := NewRenderer("main")
		r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Runtime: mode, WASMPath: "/scene.wasm", Props: json.RawMessage(`{"scene":{"materialMotionProgram":"TU9UMw=="}}`)}, gosx.Text("Scene fallback"))
		if got := r.Manifest().Engines[0].Runtime; got != string(mode) {
			t.Fatalf("runtime %q changed to %q", mode, got)
		}
	}
}
