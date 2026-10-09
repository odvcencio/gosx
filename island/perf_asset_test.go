package island

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	runtimewasm "m31labs.dev/gosx/client/runtime/wasm"
	"m31labs.dev/gosx/engine"
)

func perfAssetRendererFixture(t *testing.T) (*Renderer, *buildmanifest.Manifest) {
	t.Helper()
	ResetPreviewBootstrap()
	t.Cleanup(ResetPreviewBootstrap)
	m := &buildmanifest.Manifest{PerfAssetUses: &buildmanifest.PerfAssetUses{Version: 1, Assets: []buildmanifest.PerfAssetUse{}}}
	// Synthetic bodies isolate selection/ownership behavior from compilation.
	add := func(id, bucket, file, kind, owner string, raw []byte, size int64) buildmanifest.HashedAsset {
		sha := sha256.Sum256(raw)
		hash := buildmanifest.ContentHash(raw)
		asset := buildmanifest.HashedAsset{File: file + "." + hash + "." + kind, Hash: hash, Size: size}
		m.PerfAssetUses.Assets = append(m.PerfAssetUses.Assets, buildmanifest.PerfAssetUse{ID: id, SHA256: hex.EncodeToString(sha[:]),
			URL: buildmanifest.AssetURL("/gosx/assets", bucket, asset.File), Owner: owner, Kind: kind, Phase: "dormant", Condition: "always", Dependencies: []string{}})
		return asset
	}
	for _, entry := range []struct {
		name string
		dest *buildmanifest.HashedAsset
	}{
		{"bootstrap.js", &m.Runtime.Bootstrap}, {"bootstrap-lite.js", &m.Runtime.BootstrapLite}, {"bootstrap-runtime.js", &m.Runtime.BootstrapRuntime},
		{"wasm_exec.js", &m.Runtime.WASMExec}, {"standard-go-wasm_exec.js", &m.Runtime.StandardGoWASMExec}, {"patch.js", &m.Runtime.Patch}, {"relay.js", &m.Runtime.Relay},
		{"bootstrap-feature-islands.js", &m.Runtime.BootstrapFeatureIslands}, {"bootstrap-feature-engines.js", &m.Runtime.BootstrapFeatureEngines},
		{"bootstrap-feature-hubs.js", &m.Runtime.BootstrapFeatureHubs}, {"bootstrap-feature-controllers.js", &m.Runtime.BootstrapFeatureControllers},
		{"bootstrap-controller-input.js", &m.Runtime.BootstrapControllerInput}, {"bootstrap-feature-scene3d.js", &m.Runtime.BootstrapFeatureScene3D},
		{"bootstrap-feature-scene3d-hydrate.js", &m.Runtime.BootstrapFeatureScene3DHydrate}, {"bootstrap-feature-scene3d-webgpu.js", &m.Runtime.BootstrapFeatureScene3DWebGPU},
		{"bootstrap-feature-scene3d-webgl.js", &m.Runtime.BootstrapFeatureScene3DWebGL}, {"bootstrap-feature-scene3d-pipeline-recovery.js", &m.Runtime.BootstrapFeatureScene3DPipelineRecovery},
		{"hls.min.js", &m.Runtime.VideoHLS},
	} {
		*entry.dest = add("framework/runtime/"+entry.name, "runtime", strings.TrimSuffix(entry.name, ".js"), "js", "framework", []byte(entry.name), 10)
	}
	m.Runtime.WASMVariants = map[string]buildmanifest.RuntimeVariantAsset{}
	for i, name := range []string{"core", "engine", "collab", "full"} {
		a := add("framework/runtime/"+name+".wasm", "runtime", name, "wasm", "framework", []byte(name), int64(100*(i+1)))
		m.Runtime.WASMVariants[name] = buildmanifest.RuntimeVariantAsset{HashedAsset: a, Variant: name,
			FeatureMask: uint32(runtimewasm.RequiredFeaturesForVariant(runtimewasm.Variant(name)))}
	}
	m.Runtime.WASM = m.Runtime.WASMVariants["full"].HashedAsset
	m.Runtime.WASMIslands = add("framework/runtime/islands.wasm", "runtime", "islands", "wasm", "framework", []byte("islands"), 150)
	program := add("app/fixture/islands/Counter", "islands", "Counter", "program", "app", []byte("program"), 10)
	m.Islands = []buildmanifest.IslandAsset{{Name: "Counter", Format: "bin", HashedAsset: program}}
	nav := sha256.Sum256([]byte(runtimehost.NavigationRuntime))
	m.PerfAssetUses.Assets = append(m.PerfAssetUses.Assets, buildmanifest.PerfAssetUse{ID: "framework/runtime/navigation.js", SHA256: hex.EncodeToString(nav[:]),
		URL: runtimehost.NavigationRuntimePath, Owner: "framework", Kind: "js", Phase: "dormant", Condition: "always", Dependencies: []string{}})
	r := NewRenderer("main")
	if err := r.ApplyBuildManifest(m, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	return r, m
}

func perfAssetByID(t *testing.T, uses *buildmanifest.PerfAssetUses, id string) buildmanifest.PerfAssetUse {
	t.Helper()
	for _, a := range uses.Assets {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("missing asset %s", id)
	return buildmanifest.PerfAssetUse{}
}

func TestPerfAssetRendererSelectedRuntime(t *testing.T) {
	for _, shape := range []string{"island", "compute", "engine-shared", "scene-js", "scene-shared"} {
		t.Run(shape, func(t *testing.T) {
			r, m := perfAssetRendererFixture(t)
			backend, selected := "none", "core"
			switch shape {
			case "island":
				r.RenderIsland("Counter", nil, gosx.Text(""))
			case "compute":
				if _, err := r.RegisterComputeIsland(ComputeIslandConfig{Name: "Counter"}); err != nil {
					t.Fatal(err)
				}
			case "engine-shared":
				r.RenderEngine(engine.Config{Name: "Example", Kind: engine.KindWorker, Runtime: engine.Runtime("shared")}, gosx.Text(""))
				selected = "engine"
			case "scene-js":
				r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface}, gosx.Text(""))
				backend, selected = "webgpu", ""
			case "scene-shared":
				r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Runtime: engine.Runtime("shared")}, gosx.Text(""))
				backend, selected = "webgpu", "engine"
			}
			before := gosx.RenderHTML(r.PageHead())
			uses, err := r.PerfAssetUses(PerfAssetOptions{Backend: backend, Navigation: true})
			if err != nil {
				t.Fatal(err)
			}
			if before != gosx.RenderHTML(r.PageHead()) {
				t.Fatal("metadata added page bytes")
			}
			for _, name := range []string{"core", "engine", "collab", "full", "islands"} {
				want := "dormant"
				if name == selected {
					want = "startup"
				}
				if a := perfAssetByID(t, uses, "framework/runtime/"+name+".wasm"); a.Phase != want {
					t.Fatalf("%s selected %s: %+v", shape, selected, a)
				}
			}
			if a := perfAssetByID(t, uses, "framework/runtime/navigation.js"); a.Phase != "startup" {
				t.Fatal("navigation omitted")
			}
			if a := perfAssetByID(t, uses, "framework/runtime/bootstrap.js"); a.Phase != "dormant" {
				t.Fatal("selected bootstrap charged monolith")
			}
			if shape == "island" || shape == "compute" {
				a := perfAssetByID(t, uses, "app/fixture/islands/Counter")
				if a.Phase != "startup" || !reflect.DeepEqual(a.Dependencies, []string{"framework/runtime/core.wasm"}) {
					t.Fatalf("program dependency: %+v", a)
				}
			}
			uses.Assets[0].Dependencies = append(uses.Assets[0].Dependencies, "changed")
			if len(m.PerfAssetUses.Assets[0].Dependencies) != 0 {
				t.Fatal("producer mutated manifest graph")
			}
			again, err := r.PerfAssetUses(PerfAssetOptions{Backend: backend})
			if err != nil || perfAssetByID(t, again, "framework/runtime/navigation.js").Phase != "dormant" {
				t.Fatalf("producer leaked previous page options: %v", err)
			}
		})
	}
}

func TestPerfAssetRendererStaticAndLite(t *testing.T) {
	r, _ := perfAssetRendererFixture(t)
	uses, err := r.PerfAssetUses(PerfAssetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range uses.Assets {
		if asset.Phase != "dormant" {
			t.Fatal("static renderer acquired framework work")
		}
	}
	r.EnableBootstrap()
	uses, err = r.PerfAssetUses(PerfAssetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range uses.Assets {
		want := "dormant"
		if asset.ID == "framework/runtime/bootstrap-lite.js" {
			want = "startup"
		}
		if asset.Phase != want {
			t.Fatalf("lite use: %+v", asset)
		}
	}
}

func TestPerfAssetRendererMonolithFallbacks(t *testing.T) {
	for _, shape := range []string{"preview", "lite-missing", "selective-missing", "unconfigured-full"} {
		t.Run(shape, func(t *testing.T) {
			r, _ := perfAssetRendererFixture(t)
			switch shape {
			case "preview":
				EnablePreviewBootstrap()
			case "lite-missing":
				r.EnableBootstrap()
				r.bootstrapLitePath = ""
			case "selective-missing":
				r.RenderIsland("Counter", nil, gosx.Text(""))
				r.bootstrapRuntimePath = ""
			case "unconfigured-full":
				r.RenderIsland("Counter", nil, gosx.Text(""))
				r.bootstrapRuntimePath, r.islandRuntime.Path = "", ""
				r.runtimeVariants = nil
			}
			uses, err := r.PerfAssetUses(PerfAssetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if a := perfAssetByID(t, uses, "framework/runtime/bootstrap.js"); a.Phase != "startup" {
				t.Fatalf("fallback dropped monolith: %+v", a)
			}
			if shape == "preview" && perfAssetByID(t, uses, "framework/runtime/relay.js").Phase != "startup" {
				t.Fatal("preview omitted relay")
			}
			if shape == "unconfigured-full" && perfAssetByID(t, uses, "framework/runtime/full.wasm").Phase != "startup" {
				t.Fatal("explicit full fallback was dropped")
			}
		})
	}
}

func TestPerfAssetRendererBackendClosure(t *testing.T) {
	for _, backend := range []string{"webgpu", "webgl2"} {
		t.Run(backend, func(t *testing.T) {
			r, _ := perfAssetRendererFixture(t)
			var props json.RawMessage
			if backend == "webgl2" {
				props = json.RawMessage(`{"forceWebGL":true}`)
			}
			r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: props}, gosx.Text(""))
			uses, err := r.PerfAssetUses(PerfAssetOptions{Backend: backend})
			if err != nil {
				t.Fatal(err)
			}
			gpu, gl := perfAssetByID(t, uses, "framework/runtime/bootstrap-feature-scene3d-webgpu.js"), perfAssetByID(t, uses, "framework/runtime/bootstrap-feature-scene3d-webgl.js")
			if backend == "webgpu" {
				if gpu.Phase != "startup" || gpu.Condition != "webgpu" || gl.Phase != "after-ready" || gl.Condition != "device-loss" {
					t.Fatalf("GPU/loss closure: %+v %+v", gpu, gl)
				}
			} else if gl.Phase != "startup" || gl.Condition != "webgl" || gpu.Phase != "dormant" {
				t.Fatalf("GL closure: %+v %+v", gpu, gl)
			}
			if a := perfAssetByID(t, uses, "framework/runtime/bootstrap-feature-scene3d-pipeline-recovery.js"); a.Phase != "dormant" {
				t.Fatal("dormant recovery charged as a fetch")
			}
			if !reflect.DeepEqual(gl.Dependencies, []string{"framework/runtime/bootstrap-feature-scene3d.js"}) {
				t.Fatalf("backend prerequisite: %+v", gl)
			}
		})
	}
}

func perfWebGPULoaderRequests(t *testing.T, head string, navigatorGPU bool) []string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the emitted loader")
	}
	_, tail, found := strings.Cut(head, `data-gosx-script="feature-scene3d-webgpu-loader"`)
	if !found {
		t.Fatal("missing production WebGPU loader")
	}
	_, tail, found = strings.Cut(tail, ">")
	if !found {
		t.Fatal("missing loader body")
	}
	source, _, found := strings.Cut(tail, "</script>")
	if !found {
		t.Fatal("unterminated loader")
	}
	input, err := json.Marshal(struct {
		Source       string `json:"source"`
		NavigatorGPU bool   `json:"navigatorGPU"`
	}{source, navigatorGPU})
	if err != nil {
		t.Fatal(err)
	}
	const script = `
const assert = require('node:assert/strict');
const vm = require('node:vm');
const input = JSON.parse(require('node:fs').readFileSync(0, 'utf8'));
(async function() {
  const requests = [];
  const navigator = input.navigatorGPU ? {gpu: {requestAdapter: async () => null}} : {};
  const document = {
    readyState: 'complete',
    currentScript: {nonce: '', getAttribute() { return ''; }},
    createElement(tag) {
      assert.equal(tag, 'script');
      return {dataset: {}, setAttribute() {}};
    },
    head: {appendChild(script) { requests.push(script.src); }},
  };
  vm.runInNewContext(input.source, {window: {}, document, navigator});
  if (input.navigatorGPU) assert.equal(await navigator.gpu.requestAdapter(), null);
  process.stdout.write(JSON.stringify(requests));
})().catch(error => { console.error(error); process.exitCode = 1; });
`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "-e", script)
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("execute production loader: %v: %s", err, output)
	}
	var requests []string
	if err := json.Unmarshal(output, &requests); err != nil {
		t.Fatal(err)
	}
	return requests
}

func TestPerfAssetRendererWebGLUnusableGPUAPI(t *testing.T) {
	r, _ := perfAssetRendererFixture(t)
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface}, gosx.Text(""))
	head := gosx.RenderHTML(r.PageHead())
	t.Run("production-loader", func(t *testing.T) {
		requests := perfWebGPULoaderRequests(t, head, true)
		if !reflect.DeepEqual(requests, []string{r.bootstrapFeatureScene3dWebGPUPath}) {
			t.Fatalf("GPU API with no adapter requested %v", requests)
		}
		if requests := perfWebGPULoaderRequests(t, head, false); len(requests) != 0 {
			t.Fatalf("absent GPU API requested %v", requests)
		}
	})
	present, absent := true, false
	for _, tc := range []struct {
		name  string
		api   *bool
		phase string
	}{
		{"unknown-api", nil, "startup"},
		{"unusable-api", &present, "startup"},
		{"absent-api", &absent, "dormant"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uses, err := r.PerfAssetUses(PerfAssetOptions{Backend: "webgl2", NavigatorGPU: tc.api})
			if err != nil {
				t.Fatal(err)
			}
			gpu := perfAssetByID(t, uses, "framework/runtime/bootstrap-feature-scene3d-webgpu.js")
			gl := perfAssetByID(t, uses, "framework/runtime/bootstrap-feature-scene3d-webgl.js")
			if gpu.Phase != tc.phase || gpu.Condition != "always" || gl.Phase != "startup" || gl.Condition != "webgl" {
				t.Fatalf("GPU API download does not match WebGL use: %+v %+v", gpu, gl)
			}
			if tc.phase == "startup" && !reflect.DeepEqual(gpu.Dependencies, []string{"framework/runtime/bootstrap-feature-scene3d.js"}) {
				t.Fatalf("loader prerequisite: %+v", gpu)
			}
		})
	}
}

func TestPerfAssetRendererWebGLFallbackCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name  string
		props []json.RawMessage
		phase string
	}{
		{"webgpu-only", []json.RawMessage{json.RawMessage(`{"backendCaps":{"capable":["webgpu"]}}`)}, "dormant"},
		{"nested-webgpu-only", []json.RawMessage{json.RawMessage(`{"scene":{"backendCaps":{"capable":["webgpu"]}}}`)}, "dormant"},
		{"fallback-ignores-preference", []json.RawMessage{json.RawMessage(`{"preferWebGL":false,"backendCaps":{"capable":["webgpu","webgl"]}}`)}, "after-ready"},
		{"another-scene-allows-webgl", []json.RawMessage{json.RawMessage(`{"backendCaps":{"capable":["webgpu"]}}`), nil}, "after-ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := perfAssetRendererFixture(t)
			for _, props := range tc.props {
				r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: props}, gosx.Text(""))
			}
			uses, err := r.PerfAssetUses(PerfAssetOptions{Backend: "webgpu"})
			if err != nil {
				t.Fatal(err)
			}
			gl := perfAssetByID(t, uses, "framework/runtime/bootstrap-feature-scene3d-webgl.js")
			if gl.Phase != tc.phase || tc.phase == "after-ready" && gl.Condition != "device-loss" {
				t.Fatalf("WebGL fallback capability mismatch: %+v", gl)
			}
		})
	}
}

func TestPerfAssetRendererUnknownAndRewrite(t *testing.T) {
	for _, bad := range []string{"legacy", "missing-program", "missing-loader", "wrong-backend", "private-backend", "unsupported-backend", "missing-gpu-api"} {
		t.Run(bad, func(t *testing.T) {
			r, _ := perfAssetRendererFixture(t)
			opts := PerfAssetOptions{}
			switch bad {
			case "legacy":
				r.perfAssets = nil
			case "missing-program":
				r.RenderIsland("Unknown", nil, gosx.Text(""))
			case "missing-loader":
				r.RenderIsland("Counter", nil, gosx.Text(""))
				r.wasmExecPath = ""
			case "wrong-backend":
				opts.Backend = "webgpu"
			case "private-backend":
				opts.Backend = "private-device"
			case "unsupported-backend":
				r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(`{"forceWebGL":true}`)}, gosx.Text(""))
				opts.Backend = "webgpu"
			case "missing-gpu-api":
				r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface}, gosx.Text(""))
				present := false
				opts.Backend, opts.NavigatorGPU = "webgpu", &present
			}
			uses, err := r.PerfAssetUses(opts)
			var typed *buildmanifest.PerfAssetError
			if uses != nil || !errors.As(err, &typed) || strings.Contains(err.Error(), "private-device") {
				t.Fatalf("unknown reachability passed: %v", err)
			}
		})
	}
	r, m := perfAssetRendererFixture(t)
	if err := r.ApplyBuildManifest(m, "/assets"); err != nil {
		t.Fatal(err)
	}
	r.SetBasePath("/demo")
	r.RenderIsland("Counter", nil, gosx.Text(""))
	uses, err := r.PerfAssetUses(PerfAssetOptions{Navigation: true})
	if err != nil {
		t.Fatal(err)
	}
	if a := perfAssetByID(t, uses, "app/fixture/islands/Counter"); !strings.HasPrefix(a.URL, "/demo/assets/islands/") || a.Owner != "app" {
		t.Fatalf("rewritten identity: %+v", a)
	}
	if a := perfAssetByID(t, uses, "framework/runtime/navigation.js"); a.URL != "/demo"+runtimehost.NavigationRuntimePath {
		t.Fatalf("rewritten navigation: %+v", a)
	}
}

func TestPerfAssetNavigationTransferTransition(t *testing.T) {
	// A synthetic inline representation uses the exact current served body;
	// this is a transfer-reclassification fixture, not a historical size claim.
	r, _ := perfAssetRendererFixture(t)
	uses, err := r.PerfAssetUses(PerfAssetOptions{Navigation: true})
	if err != nil {
		t.Fatal(err)
	}
	nav := perfAssetByID(t, uses, "framework/runtime/navigation.js")
	inline := `<script data-gosx-script="navigation">` + runtimehost.NavigationRuntime + `</script>`
	external := `<script defer data-gosx-script="navigation" src="` + nav.URL + `"></script>`
	if !strings.Contains(inline, runtimehost.NavigationRuntime) || strings.Contains(external, runtimehost.NavigationRuntime) {
		t.Fatal("representations overlap")
	}
	digest := sha256.Sum256([]byte(runtimehost.NavigationRuntime))
	if nav.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("external production hash does not match the moved body")
	}
	count := 0
	for _, asset := range uses.Assets {
		if asset.ID == nav.ID && asset.Phase == "startup" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("external navigation counted more than once")
	}
}
