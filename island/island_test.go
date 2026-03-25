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

func TestPageHeadWithIslands(t *testing.T) {
	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")
	r.RenderIsland("Counter", nil, gosx.Text("0"))

	head := r.PageHead()
	html := gosx.RenderHTML(head)

	if !strings.Contains(html, "gosx-manifest") {
		t.Fatal("missing manifest in PageHead")
	}
	if !strings.Contains(html, "bootstrap.js") {
		t.Fatal("missing bootstrap script in PageHead")
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

	head := gosx.RenderHTML(r.PageHead())
	if !strings.Contains(head, "gosx-manifest") {
		t.Fatal("missing manifest for engine page")
	}
	if !strings.Contains(head, "bootstrap.js") {
		t.Fatal("missing bootstrap script for engine page")
	}
	if !strings.Contains(head, "wasm_exec.js") {
		t.Fatal("missing wasm_exec for wasm-backed engine page")
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
		JSPath:       "/gosx/engines/Whiteboard.js",
		JSExport:     "WhiteboardEngine",
		Capabilities: []engine.Capability{engine.CapCanvas, engine.CapAnimation},
		Props:        props,
	}, gosx.Text("loading"))

	html := gosx.RenderHTML(node)
	if !strings.Contains(html, `data-gosx-engine="Whiteboard"`) {
		t.Fatalf("expected engine mount markup, got %s", html)
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
	if entry.JSRef != "/gosx/engines/Whiteboard.js" {
		t.Fatalf("unexpected js ref: %s", entry.JSRef)
	}
	if entry.JSExport != "WhiteboardEngine" {
		t.Fatalf("unexpected js export: %s", entry.JSExport)
	}
	if string(entry.Props) != `{"room":"abc","stroke":2}` {
		t.Fatalf("unexpected props: %s", entry.Props)
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
	if !strings.Contains(head, "bootstrap.js") {
		t.Fatal("missing bootstrap for hub page")
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
			WASM:      buildmanifest.HashedAsset{File: "gosx-runtime.11111111.wasm", Hash: "11111111", Size: 10},
			WASMExec:  buildmanifest.HashedAsset{File: "wasm_exec.22222222.js", Hash: "22222222", Size: 20},
			Bootstrap: buildmanifest.HashedAsset{File: "bootstrap.33333333.js", Hash: "33333333", Size: 30},
			Patch:     buildmanifest.HashedAsset{File: "patch.44444444.js", Hash: "44444444", Size: 40},
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
	if !strings.Contains(headHTML, `/gosx/assets/runtime/bootstrap.33333333.js`) {
		t.Fatalf("missing hashed bootstrap path: %s", headHTML)
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
