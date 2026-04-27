package island

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/buildmanifest"
	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/hydrate"
	"github.com/odvcencio/gosx/island/program"
)

func TestRendererBasic(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")

	if r.Manifest() == nil {
		t.Fatal("nil manifest")
	}
}

func TestRenderIsland(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")

	node := r.RenderIsland("Counter", map[string]int{"initial": 0}, gosx.Text("0"))
	html := gosx.RenderHTML(node)

	if !strings.Contains(html, "data-gosx-island") {
		t.Fatal("missing island attribute")
	}
	if !strings.Contains(html, `data-gosx-enhance="island"`) || !strings.Contains(html, `data-gosx-enhance-layer="runtime"`) {
		t.Fatalf("missing island enhancement contract in %q", html)
	}
	if !strings.Contains(html, "Counter") {
		t.Fatal("missing component name")
	}
}

func TestRenderIslandWithEvents(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")

	events := []hydrate.EventSlot{
		{SlotID: "s0", EventType: "click", HandlerName: "increment"},
	}

	node := r.RenderIslandWithEvents("Counter", map[string]int{"initial": 0}, events, gosx.Text("0"))
	html := gosx.RenderHTML(node)

	if !strings.Contains(html, "data-gosx-island") {
		t.Fatal("missing island attribute")
	}
	if !strings.Contains(html, `data-gosx-enhance="island"`) {
		t.Fatalf("missing island enhancement contract in %q", html)
	}
}

func TestManifestScript(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")
	r.RenderIsland("Counter", nil, gosx.Text("0"))

	script := r.ManifestScript()
	html := gosx.RenderHTML(script)

	if !strings.Contains(html, "gosx-manifest") {
		t.Fatal("missing manifest script tag")
	}
	if strings.Contains(html, "&#34;") {
		t.Fatalf("manifest script should contain raw JSON, got %q", html)
	}
	if !strings.Contains(html, `"component": "Counter"`) {
		t.Fatalf("expected raw manifest JSON in script tag, got %q", html)
	}
}

func TestPageHeadEmpty(t *testing.T) {
	r := NewRenderer("main")
	// No islands rendered — PageHead should return empty
	head := r.PageHead()
	html := gosx.RenderHTML(head)
	if html != "" {
		t.Fatalf("expected empty for no islands, got %q", html)
	}
}

func TestPageHeadWithBootstrapOnlyUsesLiteRuntime(t *testing.T) {
	r := NewRenderer("main")
	r.EnableBootstrap()

	head := gosx.RenderHTML(r.PageHead())
	if strings.Contains(head, "gosx-manifest") {
		t.Fatal("bootstrap-only page should not emit a manifest script")
	}
	if !strings.Contains(head, "bootstrap-lite.js") {
		t.Fatal("bootstrap-only page should load the lite bootstrap runtime")
	}
	if !strings.Contains(head, `data-gosx-bootstrap-mode="lite"`) {
		t.Fatal("bootstrap-only page should mark the lite bootstrap mode")
	}
	if !strings.Contains(head, `<script defer data-gosx-script="bootstrap"`) {
		t.Fatalf("bootstrap-only page should defer the bootstrap runtime, got %s", head)
	}
	if strings.Contains(head, "wasm_exec.js") || strings.Contains(head, "patch.js") {
		t.Fatal("bootstrap-only page should not load wasm_exec or patch runtime assets")
	}
}

func TestPageHeadWithIslands(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")
	r.RenderIsland("Counter", nil, gosx.Text("0"))

	head := r.PageHead()
	html := gosx.RenderHTML(head)

	if !strings.Contains(html, "gosx-manifest") {
		t.Fatal("missing manifest in PageHead")
	}
	if !strings.Contains(html, "bootstrap-runtime.js") {
		t.Fatal("missing selective bootstrap script in PageHead")
	}
	if !strings.Contains(html, `data-gosx-script="bootstrap"`) {
		t.Fatal("missing bootstrap script role marker")
	}
	for _, snippet := range []string{
		`<script defer data-gosx-script="wasm-exec"`,
		`<script defer data-gosx-script="patch"`,
		`<script defer data-gosx-script="bootstrap"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected deferred runtime script %q in PageHead %s", snippet, html)
		}
	}
}

func TestRegisterComputeIslandUsesSharedRuntimeWithoutDOMPatch(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")
	r.SetRuntime("/gosx/runtime.wasm", "", 0)
	r.SetProgramDir("/gosx/islands")

	id, err := r.RegisterComputeIsland(ComputeIslandConfig{
		Name:                 "FightController",
		Props:                map[string]string{"match": "abc"},
		Capabilities:         []engine.Capability{engine.CapKeyboard, engine.CapGamepad},
		RequiredCapabilities: []engine.Capability{engine.CapWASM},
	})
	if err != nil {
		t.Fatalf("register compute island: %v", err)
	}
	if id != "gosx-compute-0" {
		t.Fatalf("unexpected compute island id: %s", id)
	}
	if len(r.Manifest().ComputeIslands) != 1 {
		t.Fatalf("expected one compute island, got %d", len(r.Manifest().ComputeIslands))
	}
	entry := r.Manifest().ComputeIslands[0]
	if entry.ProgramRef != "/gosx/islands/FightController.json" {
		t.Fatalf("expected inferred program ref, got %s", entry.ProgramRef)
	}
	if entry.Capabilities[1] != "gamepad" {
		t.Fatalf("unexpected capabilities: %#v", entry.Capabilities)
	}

	head := gosx.RenderHTML(r.PageHead())
	if !strings.Contains(head, "gosx-manifest") {
		t.Fatalf("compute island page should emit manifest: %s", head)
	}
	if !strings.Contains(head, "wasm_exec.js") {
		t.Fatalf("compute island page should load wasm runtime: %s", head)
	}
	if strings.Contains(head, "patch.js") {
		t.Fatalf("compute-only page should not load DOM patch runtime: %s", head)
	}

	preloads := gosx.RenderHTML(r.PreloadHints())
	if !strings.Contains(preloads, "bootstrap-feature-islands.js") {
		t.Fatalf("compute island page should preload islands feature chunk: %s", preloads)
	}
	if !strings.Contains(preloads, "/gosx/islands/FightController.json") {
		t.Fatalf("compute island program should be prefetched: %s", preloads)
	}

	summary := r.Summary()
	if summary.ComputeIslands != 1 || summary.Islands != 0 {
		t.Fatalf("unexpected summary counts: %#v", summary)
	}
}

func TestPageHeadWithEnginesOnly(t *testing.T) {
	r := NewRenderer("main")
	r.SetClientAssetPaths("/gosx/wasm_exec.js", "/gosx/patch.js", "/gosx/bootstrap.js")

	node := r.RenderEngine(engine.Config{
		Name:     "Whiteboard",
		Kind:     engine.KindSurface,
		WASMPath: "/gosx/engines/Whiteboard.wasm",
	}, gosx.Text("loading"))
	html := gosx.RenderHTML(node)
	if !strings.Contains(html, `data-gosx-engine="Whiteboard"`) {
		t.Fatalf("expected engine mount shell, got %s", html)
	}
	if !strings.Contains(html, `data-gosx-enhance="engine"`) || !strings.Contains(html, `data-gosx-enhance-layer="runtime"`) {
		t.Fatalf("expected engine enhancement contract, got %s", html)
	}

	head := gosx.RenderHTML(r.PageHead())
	if !strings.Contains(head, "gosx-manifest") {
		t.Fatal("missing manifest for engine page")
	}
	if !strings.Contains(head, "bootstrap-runtime.js") {
		t.Fatal("missing selective bootstrap script for engine page")
	}
	if !strings.Contains(head, "wasm_exec.js") {
		t.Fatal("missing wasm_exec for wasm-backed engine page")
	}
	if !strings.Contains(head, `data-gosx-script="wasm-exec"`) {
		t.Fatal("missing wasm_exec role marker")
	}
	for _, snippet := range []string{
		`<script defer data-gosx-script="wasm-exec"`,
		`<script defer data-gosx-script="bootstrap"`,
	} {
		if !strings.Contains(head, snippet) {
			t.Fatalf("expected deferred runtime script %q in engine PageHead %s", snippet, head)
		}
	}
	if strings.Contains(head, "patch.js") {
		t.Fatal("engine-only page should not load patch.js")
	}
}

func TestRenderEngineRegistersManifestEntryAndMount(t *testing.T) {
	r := NewRenderer("main")
	props := json.RawMessage(`{"room":"abc","stroke":2}`)

	node := r.RenderEngine(engine.Config{
		Name:         "Whiteboard",
		Kind:         engine.KindSurface,
		WASMPath:     "/gosx/engines/Whiteboard.wasm",
		Capabilities: []engine.Capability{engine.CapCanvas, engine.CapAnimation},
		Props:        props,
	}, gosx.Text("loading"))

	html := gosx.RenderHTML(node)
	if !strings.Contains(html, `data-gosx-engine="Whiteboard"`) {
		t.Fatalf("expected engine mount markup, got %s", html)
	}
	if !strings.Contains(html, `data-gosx-enhance="engine"`) || !strings.Contains(html, `data-gosx-fallback="server"`) {
		t.Fatalf("expected engine enhancement contract, got %s", html)
	}
	if !strings.Contains(html, `data-gosx-engine-required-capabilities="wasm"`) {
		t.Fatalf("expected wasm requirement in engine mount markup, got %s", html)
	}
	if !strings.Contains(html, `loading`) {
		t.Fatalf("expected fallback content, got %s", html)
	}

	if len(r.Manifest().Engines) != 1 {
		t.Fatalf("expected one engine entry, got %d", len(r.Manifest().Engines))
	}

	entry := r.Manifest().Engines[0]
	if entry.Component != "Whiteboard" {
		t.Fatalf("unexpected component: %s", entry.Component)
	}
	if entry.MountID == "" {
		t.Fatal("expected mount id")
	}
	if string(entry.Props) != `{"room":"abc","stroke":2}` {
		t.Fatalf("unexpected props: %s", entry.Props)
	}
	if len(entry.RequiredCapabilities) != 1 || entry.RequiredCapabilities[0] != "wasm" {
		t.Fatalf("unexpected required capabilities: %#v", entry.RequiredCapabilities)
	}
}

func TestRenderVideoEngineRegistersBuiltinManifestContract(t *testing.T) {
	r := NewRenderer("main")

	node := r.RenderEngine(engine.Config{
		Name:         "GoSXVideo",
		Kind:         engine.KindVideo,
		Capabilities: []engine.Capability{engine.CapVideo, engine.CapFetch, engine.CapAudio},
		Props:        json.RawMessage(`{"src":"/media/promo.mp4"}`),
	}, gosx.Text("loading video"))

	html := gosx.RenderHTML(node)
	for _, snippet := range []string{
		`data-gosx-engine="GoSXVideo"`,
		`data-gosx-engine-kind="video"`,
		`data-gosx-enhance="video"`,
		`data-gosx-engine-capabilities="video fetch audio"`,
		`loading video`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in video engine mount %s", snippet, html)
		}
	}

	if len(r.Manifest().Engines) != 1 {
		t.Fatalf("expected one engine entry, got %d", len(r.Manifest().Engines))
	}
	entry := r.Manifest().Engines[0]
	if entry.Kind != string(engine.KindVideo) {
		t.Fatalf("expected video kind, got %q", entry.Kind)
	}
	if entry.ProgramRef != "" {
		t.Fatalf("built-in video engine should not require a program ref, got %q", entry.ProgramRef)
	}
	if entry.MountID == "" {
		t.Fatal("expected generated mount id for video engine")
	}
	if string(entry.Props) != `{"src":"/media/promo.mp4"}` {
		t.Fatalf("unexpected props: %s", entry.Props)
	}
}

func TestRenderEngineRejectsUnsupportedKind(t *testing.T) {
	r := NewRenderer("main")

	node := r.RenderEngine(engine.Config{
		Name: "Mystery",
		Kind: engine.Kind("teleport"),
	}, gosx.Text("loading"))

	html := gosx.RenderHTML(node)
	if !strings.Contains(html, `engine error: unsupported engine kind`) {
		t.Fatalf("expected unsupported kind error, got %s", html)
	}
	if len(r.Manifest().Engines) != 0 {
		t.Fatalf("expected no manifest entry for invalid engine, got %d", len(r.Manifest().Engines))
	}
}

func TestRenderEnginePropagatesPixelSurfaceContract(t *testing.T) {
	r := NewRenderer("main")
	cfg := engine.PixelSurface("RetroBoard", 160, 144,
		engine.WithScaling(engine.ScaleFill),
		engine.WithClearColor(1, 2, 3, 255),
		engine.WithVSync(false),
	)
	cfg.WASMPath = "/gosx/engines/RetroBoard.wasm"

	node := r.RenderEngine(cfg, gosx.Text("loading"))
	html := gosx.RenderHTML(node)
	if !strings.Contains(html, `data-gosx-pixel-width="160"`) {
		t.Fatalf("expected pixel width contract, got %s", html)
	}
	if !strings.Contains(html, `data-gosx-pixel-height="144"`) {
		t.Fatalf("expected pixel height contract, got %s", html)
	}
	if !strings.Contains(html, `data-gosx-pixel-scaling="fill"`) {
		t.Fatalf("expected pixel scaling contract, got %s", html)
	}
	if !strings.Contains(html, `data-gosx-engine-required-capabilities="pixel-surface canvas wasm"`) {
		t.Fatalf("expected pixel surface required capabilities, got %s", html)
	}

	if len(r.Manifest().Engines) != 1 {
		t.Fatalf("expected one engine entry, got %d", len(r.Manifest().Engines))
	}
	entry := r.Manifest().Engines[0]
	if entry.PixelSurface == nil {
		t.Fatal("expected pixel surface manifest entry")
	}
	if entry.PixelSurface.Width != 160 || entry.PixelSurface.Height != 144 {
		t.Fatalf("unexpected pixel surface size: %#v", entry.PixelSurface)
	}
	if entry.PixelSurface.Scaling != engine.ScaleFill {
		t.Fatalf("unexpected scaling: %q", entry.PixelSurface.Scaling)
	}
	if entry.PixelSurface.VSyncEnabled() {
		t.Fatal("expected pixel surface vsync disabled")
	}
	if got := entry.RequiredCapabilities; len(got) != 3 || got[0] != "pixel-surface" || got[1] != "canvas" || got[2] != "wasm" {
		t.Fatalf("unexpected required capabilities: %#v", got)
	}
}

func TestRenderWorkerEngineRegistersWithoutDOMShell(t *testing.T) {
	r := NewRenderer("main")

	node := r.RenderEngine(engine.Config{
		Name:     "SearchIndexer",
		Kind:     engine.KindWorker,
		WASMPath: "/gosx/engines/SearchIndexer.wasm",
	}, gosx.Node{})

	if html := gosx.RenderHTML(node); html != "" {
		t.Fatalf("worker engine should not emit DOM shell, got %q", html)
	}
	if len(r.Manifest().Engines) != 1 {
		t.Fatalf("expected one worker engine entry, got %d", len(r.Manifest().Engines))
	}
	if r.Manifest().Engines[0].MountID != "" {
		t.Fatalf("worker engine should not have mount id, got %q", r.Manifest().Engines[0].MountID)
	}
}

func TestBindHubAddsManifestEntryAndBootstrapsPage(t *testing.T) {
	r := NewRenderer("main")
	r.BindHub("presence", "/gosx/hub/presence", []hydrate.HubBinding{
		{Event: "snapshot", Signal: "$presence"},
	})

	if len(r.Manifest().Hubs) != 1 {
		t.Fatalf("expected one hub entry, got %d", len(r.Manifest().Hubs))
	}
	if r.Manifest().Hubs[0].Path != "/gosx/hub/presence" {
		t.Fatalf("unexpected hub path %q", r.Manifest().Hubs[0].Path)
	}

	head := gosx.RenderHTML(r.PageHead())
	if !strings.Contains(head, "gosx-manifest") {
		t.Fatal("missing manifest for hub page")
	}
	if !strings.Contains(head, "bootstrap-runtime.js") {
		t.Fatal("missing selective bootstrap for hub page")
	}
}

func TestChecksum(t *testing.T) {
	sum1 := Checksum([]byte("hello"))
	sum2 := Checksum([]byte("hello"))
	sum3 := Checksum([]byte("world"))

	if sum1 != sum2 {
		t.Fatal("same input should produce same checksum")
	}
	if sum1 == sum3 {
		t.Fatal("different input should produce different checksum")
	}
}

func TestSerializeProps(t *testing.T) {
	data, err := SerializeProps(map[string]int{"count": 5})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "count") {
		t.Fatalf("expected 'count' in serialized props, got %q", string(data))
	}
}

func TestSerializePropsInvalid(t *testing.T) {
	_, err := SerializeProps(make(chan int))
	if err == nil {
		t.Fatal("expected error for non-serializable props")
	}
}

func TestManifestJSON(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")
	r.RenderIsland("Counter", map[string]int{"initial": 0}, gosx.Text("0"))

	jsonStr, err := r.ManifestJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonStr, "Counter") {
		t.Fatalf("expected 'Counter' in manifest JSON, got %q", jsonStr)
	}
}

func TestSetProgramAssetOverridesInferredProgramRef(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")
	r.SetProgramFormat("bin")
	r.SetProgramDir("/gosx/islands")
	r.SetProgramAsset("Counter", "/gosx/islands/Counter.abcd1234.gxi", "bin", "abcd1234")

	r.RenderIsland("Counter", nil, gosx.Text("0"))

	entry := r.Manifest().Islands[0]
	if entry.ProgramRef != "/gosx/islands/Counter.abcd1234.gxi" {
		t.Fatalf("expected hashed program ref, got %s", entry.ProgramRef)
	}
	if entry.ProgramFormat != "bin" {
		t.Fatalf("expected bin format, got %s", entry.ProgramFormat)
	}
	if entry.ProgramHash != "abcd1234" {
		t.Fatalf("expected program hash, got %s", entry.ProgramHash)
	}
}

func TestApplyBuildManifestUsesHashedRuntimeAndIslandAssets(t *testing.T) {
	r := NewRenderer("main")
	manifest := &buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			WASM:             buildmanifest.HashedAsset{File: "gosx-runtime.11111111.wasm", Hash: "11111111", Size: 10},
			WASMExec:         buildmanifest.HashedAsset{File: "wasm_exec.22222222.js", Hash: "22222222", Size: 20},
			Bootstrap:        buildmanifest.HashedAsset{File: "bootstrap.33333333.js", Hash: "33333333", Size: 30},
			BootstrapRuntime: buildmanifest.HashedAsset{File: "bootstrap-runtime.44444444.js", Hash: "44444444", Size: 31},
			Patch:            buildmanifest.HashedAsset{File: "patch.55555555.js", Hash: "55555555", Size: 40},
		},
		Islands: []buildmanifest.IslandAsset{
			{
				Name:        "Counter",
				Format:      "bin",
				HashedAsset: buildmanifest.HashedAsset{File: "Counter.55555555.gxi", Hash: "55555555", Size: 50},
			},
		},
	}

	if err := r.ApplyBuildManifest(manifest, "/gosx/assets"); err != nil {
		t.Fatalf("apply build manifest: %v", err)
	}

	r.RenderIsland("Counter", nil, gosx.Text("0"))

	headHTML := gosx.RenderHTML(r.BootstrapScript())
	if !strings.Contains(headHTML, `/gosx/assets/runtime/wasm_exec.22222222.js`) {
		t.Fatalf("missing hashed wasm_exec path: %s", headHTML)
	}
	if !strings.Contains(headHTML, `/gosx/assets/runtime/bootstrap-runtime.44444444.js`) {
		t.Fatalf("missing hashed selective bootstrap path: %s", headHTML)
	}

	entry := r.Manifest().Islands[0]
	if entry.ProgramRef != "/gosx/assets/islands/Counter.55555555.gxi" {
		t.Fatalf("expected hashed island program ref, got %s", entry.ProgramRef)
	}
	if entry.ProgramFormat != "bin" {
		t.Fatalf("expected bin format, got %s", entry.ProgramFormat)
	}
	if r.Manifest().Runtime.Path != "/gosx/assets/runtime/gosx-runtime.11111111.wasm" {
		t.Fatalf("unexpected runtime path: %s", r.Manifest().Runtime.Path)
	}
}

func TestRendererUsesIslandOnlyRuntimeForIslandPages(t *testing.T) {
	r := NewRenderer("main")
	manifest := &buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			WASM:        buildmanifest.HashedAsset{File: "gosx-runtime.full.wasm", Hash: "full", Size: 100},
			WASMIslands: buildmanifest.HashedAsset{File: "gosx-runtime-islands.slim.wasm", Hash: "slim", Size: 50},
			WASMExec:    buildmanifest.HashedAsset{File: "wasm_exec.js", Hash: "exec", Size: 20},
			Bootstrap:   buildmanifest.HashedAsset{File: "bootstrap.js", Hash: "boot", Size: 30},
			Patch:       buildmanifest.HashedAsset{File: "patch.js", Hash: "patch", Size: 40},
		},
	}
	if err := r.ApplyBuildManifest(manifest, "/gosx/assets"); err != nil {
		t.Fatalf("apply build manifest: %v", err)
	}

	r.RenderIsland("Counter", nil, gosx.Text("0"))

	manifestJSON, err := r.ManifestJSON()
	if err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	if !strings.Contains(manifestJSON, `/gosx/assets/runtime/gosx-runtime-islands.slim.wasm`) {
		t.Fatalf("expected island-only runtime in manifest: %s", manifestJSON)
	}
	if strings.Contains(manifestJSON, `/gosx/assets/runtime/gosx-runtime.full.wasm`) {
		t.Fatalf("did not expect full runtime in island-only manifest: %s", manifestJSON)
	}
}

func TestRendererKeepsFullRuntimeForSharedEnginePages(t *testing.T) {
	r := NewRenderer("main")
	manifest := &buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			WASM:        buildmanifest.HashedAsset{File: "gosx-runtime.full.wasm", Hash: "full", Size: 100},
			WASMIslands: buildmanifest.HashedAsset{File: "gosx-runtime-islands.slim.wasm", Hash: "slim", Size: 50},
			WASMExec:    buildmanifest.HashedAsset{File: "wasm_exec.js", Hash: "exec", Size: 20},
			Bootstrap:   buildmanifest.HashedAsset{File: "bootstrap.js", Hash: "boot", Size: 30},
			Patch:       buildmanifest.HashedAsset{File: "patch.js", Hash: "patch", Size: 40},
		},
	}
	if err := r.ApplyBuildManifest(manifest, "/gosx/assets"); err != nil {
		t.Fatalf("apply build manifest: %v", err)
	}

	r.RenderIsland("Counter", nil, gosx.Text("0"))
	r.RenderEngine(engine.Config{
		Name:    "SharedScene",
		Kind:    engine.KindWorker,
		Runtime: engine.RuntimeShared,
	}, gosx.Text(""))

	manifestJSON, err := r.ManifestJSON()
	if err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	if !strings.Contains(manifestJSON, `/gosx/assets/runtime/gosx-runtime.full.wasm`) {
		t.Fatalf("expected full runtime in shared-engine manifest: %s", manifestJSON)
	}
	if strings.Contains(manifestJSON, `/gosx/assets/runtime/gosx-runtime-islands.slim.wasm`) {
		t.Fatalf("did not expect island-only runtime for shared-engine manifest: %s", manifestJSON)
	}
}

func TestRendererVersionsCompatRuntimeURLsFromBuildManifest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOSX_APP_ROOT", root)

	data := []byte(`{
  "runtime": {
    "wasm": {"file": "gosx-runtime.aaaabbbb.wasm", "hash": "aaaabbbb", "size": 10},
    "wasmExec": {"file": "wasm_exec.bbbbcccc.js", "hash": "bbbbcccc", "size": 20},
    "bootstrap": {"file": "bootstrap.ccccdddd.js", "hash": "ccccdddd", "size": 30},
    "bootstrapRuntime": {"file": "bootstrap-runtime.ddddeeee.js", "hash": "ddddeeee", "size": 32},
    "patch": {"file": "patch.eeeeffff.js", "hash": "eeeeffff", "size": 40}
  },
  "islands": [],
  "css": []
}`)
	if err := os.WriteFile(filepath.Join(root, "build.json"), data, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	r := NewRenderer("main")
	r.SetRuntime("/gosx/runtime.wasm", "", 0)
	r.RenderIsland("Counter", nil, gosx.Text("0"))

	headHTML := gosx.RenderHTML(r.PageHead())
	for _, snippet := range []string{
		`/gosx/runtime.wasm?v=aaaabbbb`,
		`/gosx/assets/runtime/wasm_exec.bbbbcccc.js`,
		`/gosx/assets/runtime/patch.eeeeffff.js`,
		`/gosx/assets/runtime/bootstrap-runtime.ddddeeee.js`,
	} {
		if !strings.Contains(headHTML, snippet) {
			t.Fatalf("expected %q in versioned compat head %s", snippet, headHTML)
		}
	}
	if got := r.Manifest().Runtime.Path; got != "/gosx/runtime.wasm?v=aaaabbbb" {
		t.Fatalf("unexpected versioned runtime path %q", got)
	}
}

func TestRendererSummaryIncludesVideoHLSPathForVideoPages(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOSX_APP_ROOT", root)

	r := NewRenderer("main")
	r.RenderEngine(engine.Config{
		Name: "PromoVideo",
		Kind: engine.KindVideo,
		Props: json.RawMessage(`{
			"src": "/media/promo.m3u8"
		}`),
	}, gosx.Text("loading"))

	headHTML := gosx.RenderHTML(r.PageHead())
	if !strings.Contains(headHTML, "/gosx/bootstrap-runtime.js") {
		t.Fatalf("expected selective bootstrap for video page, got %s", headHTML)
	}
	if strings.Contains(headHTML, "/gosx/wasm_exec.js") {
		t.Fatalf("did not expect wasm exec for video page, got %s", headHTML)
	}

	summary := r.Summary()
	if summary.HLSPath != "/gosx/hls.min.js" {
		t.Fatalf("expected default hls compat path, got %q", summary.HLSPath)
	}
}

func TestNewRendererAutoLoadsBuildManifestIslandPrograms(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOSX_APP_ROOT", root)

	data := []byte(`{
  "runtime": {
    "wasm": {"file": "gosx-runtime.aaaabbbb.wasm", "hash": "aaaabbbb", "size": 10},
    "wasmExec": {"file": "wasm_exec.bbbbcccc.js", "hash": "bbbbcccc", "size": 20},
    "bootstrap": {"file": "bootstrap.ccccdddd.js", "hash": "ccccdddd", "size": 30},
    "patch": {"file": "patch.ddddeeee.js", "hash": "ddddeeee", "size": 40}
  },
  "islands": [
    {
      "name": "Counter",
      "format": "bin",
      "file": "Counter.eeeeffff.gxi",
      "hash": "eeeeffff",
      "size": 50
    }
  ],
  "css": []
}`)
	if err := os.WriteFile(filepath.Join(root, "build.json"), data, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	r := NewRenderer("main")
	r.RenderIsland("Counter", nil, gosx.Text("0"))

	entry := r.Manifest().Islands[0]
	if entry.ProgramRef != "/gosx/assets/islands/Counter.eeeeffff.gxi" {
		t.Fatalf("expected hashed program ref from default build manifest, got %s", entry.ProgramRef)
	}
	if entry.ProgramFormat != "bin" {
		t.Fatalf("expected bin program format, got %s", entry.ProgramFormat)
	}
	if r.Manifest().Runtime.Path != "/gosx/assets/runtime/gosx-runtime.aaaabbbb.wasm" {
		t.Fatalf("expected hashed runtime path from default build manifest, got %s", r.Manifest().Runtime.Path)
	}
}

func TestNewRendererAutoLoadsBuildManifestFromDistRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOSX_APP_ROOT", root)

	if err := os.MkdirAll(filepath.Join(root, "dist"), 0755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}

	data := []byte(`{
  "runtime": {
    "wasm": {"file": "gosx-runtime.11112222.wasm", "hash": "11112222", "size": 10},
    "wasmExec": {"file": "wasm_exec.22223333.js", "hash": "22223333", "size": 20},
    "bootstrap": {"file": "bootstrap.33334444.js", "hash": "33334444", "size": 30},
    "patch": {"file": "patch.44445555.js", "hash": "44445555", "size": 40}
  },
  "islands": [],
  "css": []
}`)
	if err := os.WriteFile(filepath.Join(root, "dist", "build.json"), data, 0644); err != nil {
		t.Fatalf("write dist manifest: %v", err)
	}

	r := NewRenderer("main")
	r.RenderIsland("Counter", nil, gosx.Text("0"))

	headHTML := gosx.RenderHTML(r.PageHead())
	for _, snippet := range []string{
		`/gosx/assets/runtime/gosx-runtime.11112222.wasm`,
		`/gosx/assets/runtime/wasm_exec.22223333.js`,
	} {
		if !strings.Contains(headHTML, snippet) {
			t.Fatalf("expected %q in head %s", snippet, headHTML)
		}
	}
}

func TestLoadBuildManifestFromDisk(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "build.json")
	data := []byte(`{
  "runtime": {
    "wasm": {"file": "gosx-runtime.aaaabbbb.wasm", "hash": "aaaabbbb", "size": 10},
    "wasmExec": {"file": "wasm_exec.bbbbcccc.js", "hash": "bbbbcccc", "size": 20},
    "bootstrap": {"file": "bootstrap.ccccdddd.js", "hash": "ccccdddd", "size": 30},
    "patch": {"file": "patch.ddddeeee.js", "hash": "ddddeeee", "size": 40}
  },
  "islands": [
    {"name": "Counter", "format": "json", "file": "Counter.eeeeffff.json", "hash": "eeeeffff", "size": 50}
  ],
  "css": []
}`)
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	r := NewRenderer("main")
	if err := r.LoadBuildManifest(manifestPath, "/static/assets"); err != nil {
		t.Fatalf("load build manifest: %v", err)
	}

	r.RenderIsland("Counter", nil, gosx.Text("0"))
	entry := r.Manifest().Islands[0]
	if entry.ProgramRef != "/static/assets/islands/Counter.eeeeffff.json" {
		t.Fatalf("unexpected program ref: %s", entry.ProgramRef)
	}
}

func TestRenderIslandFromProgramRendersInitialExpressions(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")
	r.SetProgramDir("/gosx/islands")

	node := r.RenderIslandFromProgram(program.CounterProgram(), nil)
	html := gosx.RenderHTML(node)

	if !strings.Contains(html, `data-gosx-island="Counter"`) {
		t.Fatal("missing island wrapper")
	}
	if !strings.Contains(html, ">0<") {
		t.Fatalf("expected initial count render, got %s", html)
	}
}

func TestRenderIslandFromProgramRendersDynamicAttrs(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")
	r.SetProgramDir("/gosx/islands")

	node := r.RenderIslandFromProgram(program.TabsProgram(), nil)
	html := gosx.RenderHTML(node)

	if !strings.Contains(html, `class="tab-btn active"`) {
		t.Fatalf("expected active tab class, got %s", html)
	}
	if !strings.Contains(html, `About: GoSX is a Go-native web platform.`) {
		t.Fatalf("expected initial tab content, got %s", html)
	}
}

func TestLoadDefaultBuildManifestRespectsSetManifestRoot(t *testing.T) {
	// Create a temp dir with a build.json
	dir := t.TempDir()
	manifestData := []byte(`{"runtime":{"bootstrap":{"file":"bootstrap.abc123.js","hash":"abc123","size":100}},"islands":[],"css":[]}`)
	if err := os.WriteFile(filepath.Join(dir, "build.json"), manifestData, 0644); err != nil {
		t.Fatalf("write build.json: %v", err)
	}

	// Set the override and verify loadDefaultBuildManifest finds it
	SetManifestRoot(dir)
	t.Cleanup(ResetManifestRoot)

	manifest := loadDefaultBuildManifest()
	if manifest == nil {
		t.Fatalf("expected manifest to load from %s, got nil", dir)
	}
	if manifest.Runtime.Bootstrap.Hash != "abc123" {
		t.Errorf("manifest.Runtime.Bootstrap.Hash = %q, want \"abc123\"", manifest.Runtime.Bootstrap.Hash)
	}
}

func TestLoadDefaultBuildManifestEmptyOverrideReturnsNil(t *testing.T) {
	// Set an empty override — represents "dev mode, source tree has no manifest"
	SetManifestRoot("")
	t.Cleanup(ResetManifestRoot)

	if manifest := loadDefaultBuildManifest(); manifest != nil {
		t.Errorf("expected nil when override is empty, got %+v", manifest)
	}
}

func TestLoadDefaultBuildManifestNoOverrideFallsBackToCWD(t *testing.T) {
	// Legacy behavior: no SetManifestRoot call, should fall back to CWD.
	// We can't easily control CWD in a test, so just verify the function
	// doesn't panic and the override path isn't active.
	ResetManifestRoot()
	// Just call it; result depends on the test runner CWD.
	_ = loadDefaultBuildManifest()
}

func TestLoadDefaultBuildManifestOverrideMissingManifestReturnsNil(t *testing.T) {
	dir := t.TempDir()
	// Directory exists but has no build.json.
	SetManifestRoot(dir)
	t.Cleanup(ResetManifestRoot)

	if manifest := loadDefaultBuildManifest(); manifest != nil {
		t.Errorf("expected nil when override dir has no manifest, got %+v", manifest)
	}
}
