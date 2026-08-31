package island

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/engine"
)

// runtimeManifestFixture returns a *buildmanifest.Manifest whose Runtime
// assets mirror a real `gosx build` output. When excludeOptionalFeatures is
// true, the scene3d (all eight chunks), video, payments, relay, and
// textlayout fields stay at their Go zero value — exactly what
// cmd/gosx/build.go now leaves behind when gosx.config.json sets
// build.runtime.exclude to those roles (see cmd/gosx/size.go
// runtimeExcludableAssetRoles). Every other runtime asset field is
// populated identically in both cases.
func runtimeManifestFixture(excludeOptionalFeatures bool) *buildmanifest.Manifest {
	asset := func(name string) buildmanifest.HashedAsset {
		return buildmanifest.HashedAsset{File: name, Hash: "deadbeef", Size: 100}
	}
	m := &buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			WASM:                        asset("gosx-runtime.deadbeef.wasm"),
			WASMExec:                    asset("wasm_exec.deadbeef.js"),
			Bootstrap:                   asset("bootstrap.deadbeef.js"),
			BootstrapLite:               asset("bootstrap-lite.deadbeef.js"),
			BootstrapRuntime:            asset("bootstrap-runtime.deadbeef.js"),
			BootstrapFeatureIslands:     asset("bootstrap-feature-islands.deadbeef.js"),
			BootstrapFeatureEngines:     asset("bootstrap-feature-engines.deadbeef.js"),
			BootstrapFeatureHubs:        asset("bootstrap-feature-hubs.deadbeef.js"),
			BootstrapFeatureControllers: asset("bootstrap-feature-controllers.deadbeef.js"),
			Patch:                       asset("patch.deadbeef.js"),
		},
	}
	if !excludeOptionalFeatures {
		m.Runtime.BootstrapFeatureTextlayout = asset("bootstrap-feature-textlayout.deadbeef.js")
		m.Runtime.BootstrapFeatureScene3D = asset("bootstrap-feature-scene3d.deadbeef.js")
		m.Runtime.BootstrapFeatureScene3DCommand = asset("bootstrap-feature-scene3d-command.deadbeef.js")
		m.Runtime.BootstrapFeatureScene3DWebGPU = asset("bootstrap-feature-scene3d-webgpu.deadbeef.js")
		m.Runtime.BootstrapFeatureScene3DWebGL = asset("bootstrap-feature-scene3d-webgl.deadbeef.js")
		m.Runtime.BootstrapFeatureScene3DGLTF = asset("bootstrap-feature-scene3d-gltf.deadbeef.js")
		m.Runtime.BootstrapFeatureScene3DAnimation = asset("bootstrap-feature-scene3d-animation.deadbeef.js")
		m.Runtime.BootstrapFeatureScene3DCompute = asset("bootstrap-feature-scene3d-compute.deadbeef.js")
		m.Runtime.BootstrapFeatureScene3DDecompress = asset("bootstrap-feature-scene3d-decompress.deadbeef.js")
		m.Runtime.VideoHLS = asset("hls.min.deadbeef.js")
		m.Runtime.StripeBridge = asset("stripe-bridge.deadbeef.js")
		m.Runtime.Relay = asset("relay.deadbeef.js")
	}
	return m
}

// writeManifestAndBuildRenderer stages manifest as dir/build.json (the
// dist/build.json shape RunBuildWithOptions writes) and returns a fresh
// Renderer that loads it via SetManifestRoot — the same path
// server.NewPageRuntime uses in production.
func writeManifestAndBuildRenderer(t *testing.T, manifest *buildmanifest.Manifest) *Renderer {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build.json"), data, 0644); err != nil {
		t.Fatalf("write build.json: %v", err)
	}
	SetManifestRoot(dir)
	t.Cleanup(ResetManifestRoot)
	return NewRenderer("main")
}

// TestExcludedRuntimeAssetsProduceNoRendererTagOrPreloadHint proves GC-3's
// framework-lane contract for island.go: a page that never uses Scene3D,
// video, payments, relay, or text-layout features renders an identical
// PageHead/PreloadHints/Summary whether or not gosx build.json carries
// those runtime asset entries. selectedBootstrapFeaturePath and
// PreloadHints gate every one of those tags on page content
// (hasSceneEngines, hasVideoEngines, plan.PreviewRelay, plan.Selective +
// manifest.Engines/Hubs/Controllers counts) — never on whether the
// manifest happened to record the asset. So build.runtime.exclude needs no
// island.go change: the renderer already omits a feature's tag and
// preload hint whenever a page does not declare that feature.
func TestExcludedRuntimeAssetsProduceNoRendererTagOrPreloadHint(t *testing.T) {
	renderPage := func(manifest *buildmanifest.Manifest) (head, preloads string, summary Summary) {
		r := writeManifestAndBuildRenderer(t, manifest)
		// A representative server-rendered page: one shared-runtime island
		// and one engine, the same features the GC-3 exclude example
		// (scene3d/video/payments/relay/textlayout) is not among.
		r.RenderIsland("Counter", map[string]int{"initial": 0}, gosx.Text("0"))
		r.RenderEngine(engine.Config{
			Name:     "Whiteboard",
			Kind:     engine.KindSurface,
			WASMPath: "/gosx/engines/Whiteboard.wasm",
		}, gosx.Text("loading"))
		head = gosx.RenderHTML(r.PageHead())
		preloads = gosx.RenderHTML(r.PreloadHints())
		summary = r.Summary()
		return
	}

	fullHead, fullPreloads, fullSummary := renderPage(runtimeManifestFixture(false))
	excludedHead, excludedPreloads, excludedSummary := renderPage(runtimeManifestFixture(true))

	if fullHead != excludedHead {
		t.Fatalf("PageHead changed when scene3d/video/payments/relay/textlayout were absent from the manifest:\nfull:     %s\nexcluded: %s", fullHead, excludedHead)
	}
	if fullPreloads != excludedPreloads {
		t.Fatalf("PreloadHints changed when scene3d/video/payments/relay/textlayout were absent from the manifest:\nfull:     %s\nexcluded: %s", fullPreloads, excludedPreloads)
	}
	if fullSummary != excludedSummary {
		t.Fatalf("Summary changed when scene3d/video/payments/relay/textlayout were absent from the manifest:\nfull:     %#v\nexcluded: %#v", fullSummary, excludedSummary)
	}

	// Sanity: this page genuinely never requests the excluded features, in
	// either manifest state — otherwise the identical-output assertions
	// above would be vacuous.
	for _, forbidden := range []string{"scene3d", "hls.min", "stripe-bridge", "relay.", "textlayout"} {
		if strings.Contains(fullHead, forbidden) || strings.Contains(fullPreloads, forbidden) {
			t.Fatalf("test page unexpectedly requested excluded feature %q even with the full manifest", forbidden)
		}
	}
}
