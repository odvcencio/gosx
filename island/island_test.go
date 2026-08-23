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
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/island/program"
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
	if strings.Contains(head, "wasm_exec.js") {
		t.Fatalf("plain JavaScript engine factories must not load a Go runtime shim: %s", head)
	}
	if !strings.Contains(head, `<script defer data-gosx-script="bootstrap"`) {
		t.Fatalf("expected deferred bootstrap in engine PageHead %s", head)
	}
	if strings.Contains(head, "patch.js") {
		t.Fatal("engine-only page should not load patch.js")
	}
}

func TestGoWASMEngineSelectsDedicatedModuleAssets(t *testing.T) {
	r := NewRenderer("main")
	r.SetRuntime("/gosx/shared-runtime.wasm", "", 0)
	r.SetClientAssetPaths("/gosx/wasm_exec.js", "/gosx/patch.js", "/gosx/bootstrap.js")
	r.SetStandardGoWASMExecPath("/gosx/standard-go-wasm_exec.js")

	node := r.RenderEngine(engine.Config{
		Name:                 "GoWASMFixture",
		Kind:                 engine.KindSurface,
		Runtime:              engine.RuntimeGoWASM,
		WASMPath:             "/assets/engines/fixture.wasm",
		RequiredCapabilities: []engine.Capability{engine.CapClipboard},
	}, gosx.Text("server fallback"))
	html := gosx.RenderHTML(node)
	if !strings.Contains(html, "data-gosx-engine=\"GoWASMFixture\"") {
		t.Fatalf("expected Go-WASM mount, got %s", html)
	}

	entry := r.Manifest().Engines[0]
	if entry.Runtime != string(engine.RuntimeGoWASM) {
		t.Fatalf("unexpected runtime %q", entry.Runtime)
	}
	if entry.ProgramRef != "/assets/engines/fixture.wasm" {
		t.Fatalf("unexpected program ref %q", entry.ProgramRef)
	}
	if got := strings.Join(entry.RequiredCapabilities, " "); got != "clipboard wasm" {
		t.Fatalf("unexpected required capabilities %q", got)
	}

	head := gosx.RenderHTML(r.PageHead())
	if !strings.Contains(head, "/gosx/standard-go-wasm_exec.js") {
		t.Fatalf("Go-WASM engine must load the isolated standard-Go shim: %s", head)
	}
	if strings.Contains(head, `data-gosx-script="wasm-exec"`) {
		t.Fatalf("Go-WASM-only route must not load the TinyGo/shared shim: %s", head)
	}
	if strings.Contains(head, "/gosx/shared-runtime.wasm") {
		t.Fatalf("dedicated Go-WASM engine must not load shared runtime: %s", head)
	}
	preloads := gosx.RenderHTML(r.PreloadHints())
	if !strings.Contains(preloads, "href=\"/assets/engines/fixture.wasm\"") {
		t.Fatalf("expected dedicated module preload: %s", preloads)
	}
}

func TestMixedIslandAndGoWASMEngineOrdersDistinctRuntimeShims(t *testing.T) {
	r := NewRenderer("main")
	r.SetClientAssetPaths("/gosx/wasm_exec.js", "/gosx/patch.js", "/gosx/bootstrap.js")
	r.SetStandardGoWASMExecPath("/gosx/standard-go-wasm_exec.js")
	r.RenderIsland("Counter", nil, gosx.Text("0"))
	r.RenderEngine(engine.Config{
		Name:     "GoWASMFixture",
		Kind:     engine.KindSurface,
		Runtime:  engine.RuntimeGoWASM,
		WASMPath: "/assets/fixture.wasm",
	}, gosx.Text("fallback"))

	head := gosx.RenderHTML(r.PageHead())
	tinyGoAt := strings.Index(head, `data-gosx-script="wasm-exec"`)
	standardGoAt := strings.Index(head, `data-gosx-script="standard-go-wasm-exec"`)
	bootstrapAt := strings.Index(head, `data-gosx-script="bootstrap"`)
	if tinyGoAt < 0 || standardGoAt < 0 || bootstrapAt < 0 || !(standardGoAt < tinyGoAt && tinyGoAt < bootstrapAt) {
		t.Fatalf("runtime scripts must be standard Go, TinyGo, then bootstrap: %s", head)
	}
	if !strings.Contains(head, `data-gosx-script="standard-go-wasm-exec" data-gosx-script-load="dom"`) {
		t.Fatalf("standard-Go shim must use CSP-safe DOM loading: %s", head)
	}
	summary := r.Summary()
	if summary.WASMExecPath == "" || summary.StandardGoWASMExecPath == "" {
		t.Fatalf("mixed runtime summary omitted a loader: %#v", summary)
	}
}

func TestRenderEngineRejectsGoWASMWithoutModule(t *testing.T) {
	r := NewRenderer("main")
	node := r.RenderEngine(engine.Config{
		Name:    "MissingModule",
		Kind:    engine.KindSurface,
		Runtime: engine.RuntimeGoWASM,
	}, gosx.Text("server fallback"))
	html := gosx.RenderHTML(node)
	if !strings.Contains(html, "requires a WASMPath") {
		t.Fatalf("expected Go-WASM path validation error, got %s", html)
	}
	if len(r.Manifest().Engines) != 0 {
		t.Fatalf("invalid Go-WASM engine reached manifest: %#v", r.Manifest().Engines)
	}
}

func TestGoWASMEnginePreloadRecognizesVersionedURLAndEscapesHref(t *testing.T) {
	r := NewRenderer("main")
	r.SetClientAssetPaths("/gosx/wasm_exec.js", "/gosx/patch.js", "/gosx/bootstrap.js")
	r.RenderEngine(engine.Config{
		Name:     "VersionedGoWASM",
		Kind:     engine.KindWorker,
		Runtime:  engine.RuntimeGoWASM,
		WASMPath: `/assets/engine.wasm?v=abc&mode=fast`,
	}, gosx.Node{})

	preloads := gosx.RenderHTML(r.PreloadHints())
	if !strings.Contains(preloads, `rel="preload"`) || !strings.Contains(preloads, `type="application/wasm"`) {
		t.Fatalf("expected a WASM preload for versioned URL: %s", preloads)
	}
	if !strings.Contains(preloads, `href="/assets/engine.wasm?v=abc&amp;mode=fast"`) {
		t.Fatalf("expected escaped versioned href: %s", preloads)
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

func TestBindHubInputAddsManifestInput(t *testing.T) {
	r := NewRenderer("main")
	r.BindHubInput("fight", "/ws/fight/abc", []hydrate.HubBinding{
		{Event: "tick", Signal: "$fight"},
	}, hydrate.HubInputConfig{
		Mode:        "fighting",
		Event:       "input",
		Player:      1,
		SendEveryMS: 16,
	})

	if len(r.Manifest().Hubs) != 1 {
		t.Fatalf("expected one hub entry, got %d", len(r.Manifest().Hubs))
	}
	if r.Manifest().Hubs[0].Input == nil {
		t.Fatal("missing hub input config")
	}
	if r.Manifest().Hubs[0].Input.Mode != "fighting" {
		t.Fatalf("unexpected hub input config %#v", r.Manifest().Hubs[0].Input)
	}
}

func TestRendererSelectsSmallestPublishedRuntimeVariant(t *testing.T) {
	r := NewRenderer("main")
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		WASM:        buildmanifest.HashedAsset{File: "gosx-runtime.full.wasm", Hash: "full", Size: 40},
		WASMIslands: buildmanifest.HashedAsset{File: "gosx-runtime-islands.wasm", Hash: "islands", Size: 5},
		WASMVariants: map[string]buildmanifest.RuntimeVariantAsset{
			"core":   {HashedAsset: buildmanifest.HashedAsset{File: "gosx-runtime-core.wasm", Hash: "core", Size: 10}, Variant: "core", FeatureMask: 17},
			"engine": {HashedAsset: buildmanifest.HashedAsset{File: "gosx-runtime-engine.wasm", Hash: "engine", Size: 25}, Variant: "engine", FeatureMask: 27},
			"collab": {HashedAsset: buildmanifest.HashedAsset{File: "gosx-runtime-collab.wasm", Hash: "collab", Size: 28}, Variant: "collab", FeatureMask: 21},
			"full":   {HashedAsset: buildmanifest.HashedAsset{File: "gosx-runtime-full.wasm", Hash: "full", Size: 40}, Variant: "full", FeatureMask: 31},
		},
	}}
	if err := r.ApplyBuildManifest(manifest, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	r.RenderIsland("Counter", nil, gosx.Text("counter"))
	if got := r.Summary().RuntimePath; got != "/gosx/assets/runtime/gosx-runtime-core.wasm" {
		t.Fatalf("island runtime = %q, want core", got)
	}
	if got := r.selectedRuntimeRef().ManifestHash; got == "" {
		t.Fatal("selected runtime omitted manifest identity")
	}

	engineNode := r.RenderEngine(engine.Config{Name: "Board", Kind: engine.KindSurface, Runtime: engine.RuntimeShared}, gosx.Node{})
	_ = engineNode
	if got := r.Summary().RuntimePath; got != "/gosx/assets/runtime/gosx-runtime-engine.wasm" {
		t.Fatalf("island plus engine runtime = %q, want engine", got)
	}
}

func TestSetClientIdentityAddsManifestConfig(t *testing.T) {
	r := NewRenderer("main")
	r.SetClientIdentity(hydrate.ClientIdentityConfig{
		CookieName: "test_client",
		HeaderName: "X-Test-Client",
		Prefix:     "tc-",
	})

	if r.Manifest().ClientIdentity == nil {
		t.Fatal("missing client identity config")
	}
	if r.Manifest().ClientIdentity.HeaderName != "X-Test-Client" {
		t.Fatalf("unexpected identity config %#v", r.Manifest().ClientIdentity)
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
			WASM:               buildmanifest.HashedAsset{File: "gosx-runtime.11111111.wasm", Hash: "11111111", Size: 10},
			WASMExec:           buildmanifest.HashedAsset{File: "wasm_exec.22222222.js", Hash: "22222222", Size: 20},
			StandardGoWASMExec: buildmanifest.HashedAsset{File: "standard-go-wasm_exec.2a2a2a2a.js", Hash: "2a2a2a2a", Size: 21},
			Bootstrap:          buildmanifest.HashedAsset{File: "bootstrap.33333333.js", Hash: "33333333", Size: 30},
			BootstrapRuntime:   buildmanifest.HashedAsset{File: "bootstrap-runtime.44444444.js", Hash: "44444444", Size: 31},
			Patch:              buildmanifest.HashedAsset{File: "patch.55555555.js", Hash: "55555555", Size: 40},
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

func TestScene3DWebGPUFeatureLoaderCarriesGoSXScriptProvenance(t *testing.T) {
	r := NewRenderer("main")
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		Bootstrap:                      buildmanifest.HashedAsset{File: "bootstrap.js", Hash: "boot"},
		BootstrapRuntime:               buildmanifest.HashedAsset{File: "bootstrap-runtime.js", Hash: "runtime"},
		BootstrapFeatureEngines:        buildmanifest.HashedAsset{File: "bootstrap-feature-engines.js", Hash: "engines"},
		BootstrapFeatureScene3D:        buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d.js", Hash: "scene"},
		BootstrapFeatureScene3DCommand: buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-command.js", Hash: "command"},
		BootstrapFeatureScene3DWebGPU:  buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-webgpu.js", Hash: "webgpu"},
	}}
	if err := r.ApplyBuildManifest(manifest, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface}, gosx.Text(""))
	html := gosx.RenderHTML(r.BootstrapScript())
	if !strings.Contains(html, `data-gosx-script="feature-scene3d-webgpu-loader"`) {
		t.Fatalf("Scene3D WebGPU loader lacks GoSX provenance: %s", html)
	}
	if !strings.Contains(html, `data-gosx-scene3d-command-url="/gosx/assets/runtime/bootstrap-feature-scene3d-command.js"`) {
		t.Fatalf("Scene3D script lacks command chunk URL: %s", html)
	}
}

func TestScene3DWebGPUFeatureLoaderPropagatesNonceToDynamicScript(t *testing.T) {
	r := NewRenderer("main")
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		Bootstrap:                      buildmanifest.HashedAsset{File: "bootstrap.js", Hash: "boot"},
		BootstrapRuntime:               buildmanifest.HashedAsset{File: "bootstrap-runtime.js", Hash: "runtime"},
		BootstrapFeatureEngines:        buildmanifest.HashedAsset{File: "bootstrap-feature-engines.js", Hash: "engines"},
		BootstrapFeatureScene3D:        buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d.js", Hash: "scene"},
		BootstrapFeatureScene3DCommand: buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-command.js", Hash: "command"},
		BootstrapFeatureScene3DWebGPU:  buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-webgpu.js", Hash: "webgpu"},
	}}
	if err := r.ApplyBuildManifest(manifest, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface}, gosx.Text(""))

	html := gosx.RenderHTML(r.BootstrapScriptWithNonce("scene-nonce"))
	for _, snippet := range []string{
		`data-gosx-script="feature-scene3d-webgpu-loader" nonce="scene-nonce"`,
		`document.currentScript.nonce`,
		`if(_n){s.nonce=_n;}`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected Scene3D WebGPU loader to include %q in %s", snippet, html)
		}
	}
}

// TestPreloadHintsSkipScene3DAlreadyEmittedAsScriptTag: PreloadHints() must
// not <link rel="preload" as="script"> a bundle BootstrapScript() ALSO emits
// as a same-document <script defer src="..."> tag — the browser's preload
// scanner already discovers deferred scripts at full priority during HTML
// parsing, so the extra preload adds nothing and, on a heavy page that
// delays script execution, trips Firefox's "preloaded but not used within a
// few seconds" warning. scene3d is the one selective feature bundle that IS
// always also emitted as a <script defer> tag (see BootstrapScript); engines/
// hubs/islands are fetched by client-side JS after bootstrap.js runs, so
// their preloads remain genuinely useful and must NOT be dropped.
func TestPreloadHintsSkipScene3DAlreadyEmittedAsScriptTag(t *testing.T) {
	r := NewRenderer("main")
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		Bootstrap:               buildmanifest.HashedAsset{File: "bootstrap.js", Hash: "boot"},
		BootstrapRuntime:        buildmanifest.HashedAsset{File: "bootstrap-runtime.js", Hash: "runtime"},
		BootstrapFeatureEngines: buildmanifest.HashedAsset{File: "bootstrap-feature-engines.js", Hash: "engines"},
		BootstrapFeatureHubs:    buildmanifest.HashedAsset{File: "bootstrap-feature-hubs.js", Hash: "hubs"},
		BootstrapFeatureScene3D: buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d.js", Hash: "scene"},
	}}
	if err := r.ApplyBuildManifest(manifest, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface}, gosx.Text(""))

	preloads := gosx.RenderHTML(r.PreloadHints())
	if strings.Contains(preloads, "bootstrap-feature-scene3d") {
		t.Fatalf("scene3d bundle must not be preloaded — it is already a same-document <script defer> tag: %s", preloads)
	}
	if !strings.Contains(preloads, "bootstrap-feature-engines") {
		t.Fatalf("engines bundle preload must be preserved (client-side fetched, never a same-document script tag): %s", preloads)
	}

	scripts := gosx.RenderHTML(r.BootstrapScript())
	if !strings.Contains(scripts, `data-gosx-script="feature-scene3d"`) || !strings.Contains(scripts, "bootstrap-feature-scene3d") {
		t.Fatalf("scene3d bundle must still be emitted as a deferred <script> tag: %s", scripts)
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

func TestRendererUsesIslandOnlyRuntimeForComputeIslandPages(t *testing.T) {
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
	if _, err := r.RegisterComputeIsland(ComputeIslandConfig{
		Name:                 "AIMatchmaker",
		Capabilities:         []engine.Capability{engine.CapCompute, engine.CapKeyboard},
		RequiredCapabilities: []engine.Capability{engine.CapWASM},
	}); err != nil {
		t.Fatalf("register compute island: %v", err)
	}

	manifestJSON, err := r.ManifestJSON()
	if err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	if !strings.Contains(manifestJSON, `/gosx/assets/runtime/gosx-runtime-islands.slim.wasm`) {
		t.Fatalf("expected island-only runtime in compute manifest: %s", manifestJSON)
	}
	if strings.Contains(manifestJSON, `/gosx/assets/runtime/gosx-runtime.full.wasm`) {
		t.Fatalf("did not expect full runtime in compute-only manifest: %s", manifestJSON)
	}

	headHTML := gosx.RenderHTML(r.PageHead())
	if strings.Contains(headHTML, `/gosx/assets/runtime/patch.patch.js`) {
		t.Fatalf("compute-only page should not load DOM patch runtime: %s", headHTML)
	}
	summary := r.Summary()
	if summary.RuntimePath != "/gosx/assets/runtime/gosx-runtime-islands.slim.wasm" {
		t.Fatalf("expected slim compute runtime path, got %#v", summary)
	}
	if summary.PatchPath != "" {
		t.Fatalf("compute-only summary should not require patch runtime, got %#v", summary)
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

// hasIslandManagedFormAttr reports whether html contains the bare
// gosx.ManagedFormAttr contract attribute as its own attribute, not merely
// as a prefix of a longer attribute sharing the same name (for example
// data-gosx-form-state) — see hasManagedFormAttr in
// route/managed_form_shorthand_test.go for the same rule applied to the
// file-program renderer's output.
func hasIslandManagedFormAttr(html string) bool {
	return strings.Contains(html, " "+gosx.ManagedFormAttr+" ") ||
		strings.Contains(html, " "+gosx.ManagedFormAttr+">")
}

// TestRenderIslandFromProgramExpandsManagedFormShorthand covers gosx#179
// F2: the island renderer was a third form render surface that never
// expanded data-gosx-managed. renderResolvedNodeInto (called through
// RenderIslandFromProgram -> renderProgramHTML -> RenderResolvedHTML) used
// to copy a resolved <form> node's attributes straight through, so an
// island form authored with the shorthand served with the raw attribute
// instead of the managed-form contract, unlike the same shorthand on a
// .gsx page rendered outside an island (route/fileprogram.go) or through
// the Go Node API (node.go). The navigation runtime still intercepted the
// unexpanded form client-side, so this was never a fail-open bug — only a
// mismatch between what the three render surfaces served.
func TestRenderIslandFromProgramExpandsManagedFormShorthand(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")
	r.SetProgramDir("/gosx/islands")

	prog := &program.Program{
		Name: "ShorthandForm",
		Nodes: []program.Node{
			{ // 0: form root, bare shorthand
				Kind: program.NodeElement,
				Tag:  "form",
				Attrs: []program.Attr{
					{Kind: program.AttrStatic, Name: "method", Value: "post"},
					{Kind: program.AttrStatic, Name: "action", Value: "/gosx/action/y"},
					{Kind: program.AttrBool, Name: "data-gosx-managed"},
				},
				Children: []program.NodeID{1},
			},
			{ // 1: input
				Kind: program.NodeElement,
				Tag:  "input",
				Attrs: []program.Attr{
					{Kind: program.AttrStatic, Name: "name", Value: "q"},
				},
			},
		},
		Root: 0,
	}

	node := r.RenderIslandFromProgram(prog, nil)
	html := gosx.RenderHTML(node)

	for _, want := range []string{
		`method="post"`,
		`action="/gosx/action/y"`,
		gosx.ManagedFormStateAttr + `="idle"`,
		gosx.EnhancementAttr + `="form"`,
		gosx.EnhancementLayerAttr + `="bootstrap"`,
		gosx.RuntimeFallbackAttr + `="native-form"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in island-rendered managed-shorthand form html %q", want, html)
		}
	}
	if !hasIslandManagedFormAttr(html) {
		t.Fatalf("expected the bare %s attribute in island-rendered form html %q", gosx.ManagedFormAttr, html)
	}
	if strings.Contains(html, gosx.ManagedFormShorthandAttr) {
		t.Fatalf("expected shorthand attribute removed from island output, got %q", html)
	}
	// The shorthand must not add a mode attribute — the HTML method
	// attribute stays authoritative, matching node.go's and the
	// file-program renderer's expansion rule.
	if strings.Contains(html, gosx.ManagedFormModeAttr) {
		t.Fatalf("expected no %s from the shorthand alone, got %q", gosx.ManagedFormModeAttr, html)
	}
}

// TestRenderIslandFromProgramManagedFormShorthandFalseOptsOut is the
// island-render counterpart to the .gsx and Node-API opt-out tests: a
// falsy shorthand value must not expand, and must survive unchanged.
func TestRenderIslandFromProgramManagedFormShorthandFalseOptsOut(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")
	r.SetProgramDir("/gosx/islands")

	prog := &program.Program{
		Name: "ShorthandFormOptOut",
		Nodes: []program.Node{
			{
				Kind: program.NodeElement,
				Tag:  "form",
				Attrs: []program.Attr{
					{Kind: program.AttrStatic, Name: "action", Value: "/gosx/action/y"},
					{Kind: program.AttrStatic, Name: "data-gosx-managed", Value: "false"},
				},
			},
		},
		Root: 0,
	}

	node := r.RenderIslandFromProgram(prog, nil)
	html := gosx.RenderHTML(node)

	if !strings.Contains(html, `data-gosx-managed="false"`) {
		t.Fatalf("expected the literal opt-out attribute in island output, got %q", html)
	}
	if hasIslandManagedFormAttr(html) {
		t.Fatalf("expected no expansion for data-gosx-managed=\"false\" in island output, got %q", html)
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

// TestScene3DScriptCarriesLazyChunkURLs pins that the feature-scene3d script tag
// advertises the hashed URL of every chunk the browser fetches on demand.
//
// The WebGL2 renderer moved out of the base Scene3D chunk so a WebGPU-capable
// browser stops downloading roughly 34,000 brotli bytes it never runs. That
// saving only lands if the runtime can find the chunk. Without a hashed URL the
// runtime falls back to the unversioned path, which serves correctly but cannot
// be cached immutably; without any URL the fetch 404s and the page silently
// drops to the legacy vertex-colour renderer instead of PBR.
func TestScene3DScriptCarriesLazyChunkURLs(t *testing.T) {
	r := NewRenderer("main")
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		Bootstrap:                        buildmanifest.HashedAsset{File: "bootstrap.js", Hash: "boot"},
		BootstrapRuntime:                 buildmanifest.HashedAsset{File: "bootstrap-runtime.js", Hash: "runtime"},
		BootstrapFeatureEngines:          buildmanifest.HashedAsset{File: "bootstrap-feature-engines.js", Hash: "engines"},
		BootstrapFeatureScene3D:          buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d.js", Hash: "scene"},
		BootstrapFeatureScene3DCommand:   buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-command.js", Hash: "command"},
		BootstrapFeatureScene3DWebGPU:    buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-webgpu.js", Hash: "webgpu"},
		BootstrapFeatureScene3DWebGL:     buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-webgl.js", Hash: "webgl"},
		BootstrapFeatureScene3DGLTF:      buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-gltf.js", Hash: "gltf"},
		BootstrapFeatureScene3DAnimation: buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-animation.js", Hash: "anim"},
	}}
	if err := r.ApplyBuildManifest(manifest, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface}, gosx.Text(""))
	html := gosx.RenderHTML(r.BootstrapScript())

	for _, want := range []string{
		`data-gosx-scene3d-webgl-url="/gosx/assets/runtime/bootstrap-feature-scene3d-webgl.js"`,
		`data-gosx-scene3d-webgpu-url="/gosx/assets/runtime/bootstrap-feature-scene3d-webgpu.js"`,
		`data-gosx-scene3d-gltf-url="/gosx/assets/runtime/bootstrap-feature-scene3d-gltf.js"`,
		`data-gosx-scene3d-animation-url="/gosx/assets/runtime/bootstrap-feature-scene3d-animation.js"`,
		`data-gosx-scene3d-command-url="/gosx/assets/runtime/bootstrap-feature-scene3d-command.js"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("feature-scene3d script is missing %s\ngot: %s", want, html)
		}
	}

	// The WebGL renderer must not be emitted as a script tag. It loads on
	// demand, and a tag here would defeat the whole saving.
	if strings.Contains(html, `src="/gosx/assets/runtime/bootstrap-feature-scene3d-webgl.js"`) {
		t.Errorf("WebGL chunk is eagerly loaded as a script tag: %s", html)
	}
}

// TestTextlayoutChunkIsNeverEmittedEagerly pins that the demand-loaded
// text-layout engine gets no script tag and no preload hint. Either one would
// download the chunk on every page and cancel the saving the client-side gate
// exists to capture.
func TestTextlayoutChunkIsNeverEmittedEagerly(t *testing.T) {
	r := NewRenderer("main")
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		Bootstrap:                  buildmanifest.HashedAsset{File: "bootstrap.js", Hash: "boot"},
		BootstrapRuntime:           buildmanifest.HashedAsset{File: "bootstrap-runtime.js", Hash: "runtime"},
		BootstrapFeatureIslands:    buildmanifest.HashedAsset{File: "bootstrap-feature-islands.js", Hash: "islands"},
		BootstrapFeatureTextlayout: buildmanifest.HashedAsset{File: "bootstrap-feature-textlayout.js", Hash: "textlayout"},
	}}
	if err := r.ApplyBuildManifest(manifest, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	r.RenderIsland("Counter", nil, gosx.Text(""))
	scripts := gosx.RenderHTML(r.BootstrapScript())
	hints := gosx.RenderHTML(r.PreloadHints())

	if strings.Contains(scripts, "bootstrap-feature-textlayout") {
		t.Errorf("text-layout chunk emitted as a script tag: %s", scripts)
	}
	if strings.Contains(hints, "bootstrap-feature-textlayout") {
		t.Errorf("text-layout chunk emitted as a preload hint: %s", hints)
	}
	// The resolved hashed URL must still be available to the runtime.
	if got := r.BootstrapFeatureTextlayoutPath(); got != "/gosx/assets/runtime/bootstrap-feature-textlayout.js" {
		t.Errorf("text-layout chunk URL not resolved from the manifest: %q", got)
	}
}

// scene3DChunkGateRenderer builds a renderer with every Scene3D chunk resolved
// from a manifest, then registers one GoSXScene3D engine with the given props.
// It returns the rendered bootstrap script markup.
func scene3DChunkGateRenderer(t *testing.T, props any) string {
	t.Helper()
	r := NewRenderer("main")
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		Bootstrap:                         buildmanifest.HashedAsset{File: "bootstrap.js", Hash: "boot"},
		BootstrapRuntime:                  buildmanifest.HashedAsset{File: "bootstrap-runtime.js", Hash: "runtime"},
		BootstrapFeatureEngines:           buildmanifest.HashedAsset{File: "bootstrap-feature-engines.js", Hash: "engines"},
		BootstrapFeatureScene3D:           buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d.js", Hash: "scene"},
		BootstrapFeatureScene3DCompute:    buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-compute.js", Hash: "compute"},
		BootstrapFeatureScene3DDecompress: buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-decompress.js", Hash: "decompress"},
	}}
	if err := r.ApplyBuildManifest(manifest, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(props)
	if err != nil {
		t.Fatal(err)
	}
	r.RenderEngine(engine.Config{
		Name:  "GoSXScene3D",
		Kind:  engine.KindSurface,
		Props: raw,
	}, gosx.Text(""))
	return gosx.RenderHTML(r.BootstrapScript())
}

const (
	scene3DComputeURLAttr    = `data-gosx-scene3d-compute-url="/gosx/assets/runtime/bootstrap-feature-scene3d-compute.js"`
	scene3DDecompressURLAttr = `data-gosx-scene3d-decompress-url="/gosx/assets/runtime/bootstrap-feature-scene3d-decompress.js"`
)

// TestMinimalSceneGetsNoComputeOrDecompressURL is the headline of the "no free
// lunch" rule. A cube and one directional light run no particle simulation, no
// GPU cull, no quantized decoder and no point generator. The page must not
// advertise either chunk, because the runtime refuses to guess a path and can
// therefore never fetch bytes this scene cannot use.
func TestMinimalSceneGetsNoComputeOrDecompressURL(t *testing.T) {
	markup := scene3DChunkGateRenderer(t, map[string]any{
		"scene": map[string]any{
			"objects": []map[string]any{{"id": "cube", "kind": "box"}},
			"lights":  []map[string]any{{"id": "key", "kind": "directional"}},
		},
	})
	if strings.Contains(markup, "bootstrap-feature-scene3d-compute") {
		t.Errorf("a cube and a directional light must not advertise the compute chunk:\n%s", markup)
	}
	if strings.Contains(markup, "bootstrap-feature-scene3d-decompress") {
		t.Errorf("a cube and a directional light must not advertise the decompress chunk:\n%s", markup)
	}
}

// TestSceneWithParticlesReceivesTheComputeURL pins the other half of the gate.
// A chunk that is needed and not advertised is a dropped feature, not a smaller
// page: the runtime rejects rather than guesses, so a missing URL means the
// particles never appear.
func TestSceneWithParticlesReceivesTheComputeURL(t *testing.T) {
	markup := scene3DChunkGateRenderer(t, map[string]any{
		"scene": map[string]any{
			"computeParticles": []map[string]any{{"id": "sparks", "count": 512}},
		},
	})
	if !strings.Contains(markup, scene3DComputeURLAttr) {
		t.Errorf("a scene with a compute particle system must advertise the compute chunk:\n%s", markup)
	}
	if strings.Contains(markup, "bootstrap-feature-scene3d-decompress") {
		t.Errorf("plain particle arrays need no decompress chunk:\n%s", markup)
	}
}

// TestSceneWithInstancedMeshReceivesTheComputeURL covers the second path into
// the compute chunk. The WebGPU renderer culls instanced meshes on the GPU with
// a kernel that ships in the same file as the particle systems.
func TestSceneWithInstancedMeshReceivesTheComputeURL(t *testing.T) {
	markup := scene3DChunkGateRenderer(t, map[string]any{
		"scene": map[string]any{
			"instancedMeshes": []map[string]any{{"id": "meteors", "instanceCount": 4096}},
		},
	})
	if !strings.Contains(markup, scene3DComputeURLAttr) {
		t.Errorf("a scene with an instanced mesh must advertise the compute chunk:\n%s", markup)
	}
}

// TestSceneWithCompressedPointsReceivesTheDecompressURL pins the decompress
// gate. createSceneState decodes every compressed array as its first statement,
// so a scene that carries one and gets no URL renders nothing at all.
func TestSceneWithCompressedPointsReceivesTheDecompressURL(t *testing.T) {
	markup := scene3DChunkGateRenderer(t, map[string]any{
		"scene": map[string]any{
			"points": []map[string]any{{
				"id":                  "galaxy",
				"compressedPositions": []map[string]any{{"packed": "AAAA", "dim": 3}},
			}},
		},
	})
	if !strings.Contains(markup, scene3DDecompressURLAttr) {
		t.Errorf("a scene with a compressed point layer must advertise the decompress chunk:\n%s", markup)
	}
	if strings.Contains(markup, "bootstrap-feature-scene3d-compute") {
		t.Errorf("a point layer is not a particle system and needs no compute chunk:\n%s", markup)
	}
}

// TestSceneWithGeneratedPointsReceivesTheDecompressURL covers the second path
// into the decompress chunk: the procedural point generators ship beside the
// decoder because each calls the other.
func TestSceneWithGeneratedPointsReceivesTheDecompressURL(t *testing.T) {
	markup := scene3DChunkGateRenderer(t, map[string]any{
		"scene": map[string]any{
			"points": []map[string]any{{
				"id":        "starfield",
				"count":     20000,
				"generator": map[string]any{"kind": "box", "seed": 7},
			}},
		},
	})
	if !strings.Contains(markup, scene3DDecompressURLAttr) {
		t.Errorf("a generated point layer must advertise the decompress chunk:\n%s", markup)
	}
}

// TestCompressionPolicyAloneReceivesTheDecompressURL covers progressive and
// level-of-detail mode. Both drive the chunk after the first frame even when
// the first payload arrives as plain float arrays.
func TestCompressionPolicyAloneReceivesTheDecompressURL(t *testing.T) {
	markup := scene3DChunkGateRenderer(t, map[string]any{
		"compression": map[string]any{"lod": true, "lodThreshold": 20},
		"scene": map[string]any{
			"points": []map[string]any{{"id": "cloud", "positions": []float64{0, 0, 0}}},
		},
	})
	if !strings.Contains(markup, scene3DDecompressURLAttr) {
		t.Errorf("a level-of-detail policy must advertise the decompress chunk:\n%s", markup)
	}
}

// TestGatedScene3DChunksAreNeverEmittedEagerly is the sibling of
// TestTextlayoutChunkIsNeverEmittedEagerly. An eager script tag or a preload
// hint would download the chunk on every page and cancel the saving silently:
// no size budget measures per-page transfer, so nothing else can see it.
func TestGatedScene3DChunksAreNeverEmittedEagerly(t *testing.T) {
	r := NewRenderer("main")
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		Bootstrap:                         buildmanifest.HashedAsset{File: "bootstrap.js", Hash: "boot"},
		BootstrapRuntime:                  buildmanifest.HashedAsset{File: "bootstrap-runtime.js", Hash: "runtime"},
		BootstrapFeatureEngines:           buildmanifest.HashedAsset{File: "bootstrap-feature-engines.js", Hash: "engines"},
		BootstrapFeatureScene3D:           buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d.js", Hash: "scene"},
		BootstrapFeatureScene3DCompute:    buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-compute.js", Hash: "compute"},
		BootstrapFeatureScene3DDecompress: buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-decompress.js", Hash: "decompress"},
	}}
	if err := r.ApplyBuildManifest(manifest, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"compression": map[string]any{"lod": true},
		"scene": map[string]any{
			"computeParticles": []map[string]any{{"id": "sparks"}},
			"points":           []map[string]any{{"id": "cloud", "generator": map[string]any{"kind": "box"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: raw}, gosx.Text(""))
	scripts := gosx.RenderHTML(r.BootstrapScript())
	hints := gosx.RenderHTML(r.PreloadHints())

	// Both URLs must be advertised for this scene.
	for _, want := range []string{scene3DComputeURLAttr, scene3DDecompressURLAttr} {
		if !strings.Contains(scripts, want) {
			t.Fatalf("the gate did not advertise %s:\n%s", want, scripts)
		}
	}
	for _, chunk := range []string{
		"bootstrap-feature-scene3d-compute.js",
		"bootstrap-feature-scene3d-decompress.js",
	} {
		if strings.Contains(scripts, `src="/gosx/assets/runtime/`+chunk+`"`) {
			t.Errorf("%s is loaded eagerly as a script tag, which cancels the split:\n%s", chunk, scripts)
		}
		if strings.Contains(hints, chunk) {
			t.Errorf("%s is emitted as a preload hint, which downloads it on every page:\n%s", chunk, hints)
		}
	}
}
