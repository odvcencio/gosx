package island

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	"m31labs.dev/gosx/controller"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/scene"
)

//go:generate go test . -run ^TestPerfLoaderCorpus$ -args -update-perf-loader

var updatePerfLoader = flag.Bool("update-perf-loader", false, "update loader differential fixtures")

type loaderAsset struct {
	URL    string `json:"url"`
	Source string `json:"source,omitempty"`
	Body   []byte `json:"body,omitempty"`
}
type loaderPair struct {
	Stream     []byte        `json:"stream,omitempty"`
	Variant    string        `json:"variant"`
	Features   []string      `json:"features"`
	Base       string        `json:"base"`
	GPU        string        `json:"gpu"`
	Triggers   []string      `json:"triggers"`
	Startup    []string      `json:"startup"`
	AfterReady []string      `json:"afterReady"`
	Dormant    []string      `json:"dormant"`
	Assets     []loaderAsset `json:"assets"`
}

// All page markup and manifests come from Renderer. Native VM execution is a
// harness boundary; every JavaScript loader and runtime chunk is production
// code, bound to its complete committed body rather than a synthetic name.
func TestPerfLoaderCorpus(t *testing.T) {
	const seed = 52904
	features := []string{"empty", "island", "compute", "engine", "shared-engine", "hub", "controller", "controller-input", "text", "navigation",
		"scene-gpu", "scene-webgl", "gpu-fallback", "gpu-only", "zoom", "label", "walk", "vessel", "particles", "compressed", "ktx", "model",
		"command", "timeline", "burst", "instance-stream", "animation", "late-text", "video-native", "video-hls", "all"}
	cells := [][]string{}
	for _, name := range features {
		cells = append(cells, []string{name})
	}
	rng := rand.New(rand.NewSource(seed))
	for i := 0; i < 8; i++ {
		cell := []string{"scene-gpu"}
		for _, name := range []string{"island", "compute", "shared-engine", "hub", "controller-input", "text", "zoom", "label", "navigation"} {
			if rng.Intn(2) == 0 {
				cell = append(cell, name)
			}
		}
		cells = append(cells, cell)
	}
	expected := map[string][]byte{}
	documents := []json.RawMessage{}
	for _, variant := range []string{"monolith", "scene-monolith", "lite", "selective"} {
		for _, base := range []string{"", "/demo", "/a/b"} {
			for _, features := range cells {
				name := fmt.Sprintf("%03d", len(expected)/2)
				page, pair := perfLoaderPage(t, variant, base, features)
				data, err := json.Marshal(pair)
				if err != nil {
					t.Fatal(err)
				}
				documents = append(documents, json.RawMessage(page))
				expected[name+".html"], expected[name+".json"] = nil, append(data, '\n')
			}
		}
	}
	input, _ := json.Marshal(documents)
	cmd := exec.Command("go", "run", "../internal/perfloaderfixture")
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("production document renderer: %v: %s", err, stderr.String())
	}
	var pages []string
	if err := json.Unmarshal(output, &pages); err != nil || len(pages) != len(documents) {
		t.Fatalf("production document output: %v", err)
	}
	for i, page := range pages {
		expected[fmt.Sprintf("%03d.html", i)] = []byte(page)
	}
	dir := filepath.Join("..", "client", "js", "testdata", "perf-loader")
	if *updatePerfLoader {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range expected {
		file := filepath.Join(dir, name)
		if *updatePerfLoader {
			if err := os.WriteFile(file, data, 0644); err != nil {
				t.Fatal(err)
			}
		} else if got, err := os.ReadFile(file); err != nil || !bytes.Equal(got, data) {
			t.Fatalf("loader fixture %s is stale; run GOWORK=off go generate ./island", name)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if _, ok := expected[entry.Name()]; !ok {
			if *updatePerfLoader {
				if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Fatalf("unexpected loader fixture %s", entry.Name())
			}
		}
	}
	t.Logf("seed=%d pairs=%d", seed, len(expected)/2)
}

func perfLoaderPage(t *testing.T, variant, base string, features []string) (string, loaderPair) {
	t.Helper()
	r, m := perfAssetRendererFixture(t)
	bodies, sources := perfLoaderBodies(t, m)
	if err := r.ApplyBuildManifest(m, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	if variant == "monolith" || variant == "scene-monolith" {
		r.bootstrapRuntimePath, r.bootstrapLitePath = "", ""
		if variant == "scene-monolith" {
			r.bootstrapFeatureScene3dPath = ""
		}
	}
	r.SetBasePath(base)
	r.EnableBootstrap()
	pair := loaderPair{Variant: variant, Features: features, Base: base, GPU: "absent", Triggers: []string{}, Startup: []string{}, AfterReady: []string{}, Dormant: []string{}, Assets: []loaderAsset{}}
	set := map[string]bool{}
	for _, name := range features {
		set[name] = true
	}
	if set["all"] {
		for _, name := range []string{"island", "compute", "engine", "shared-engine", "hub", "controller-input", "text", "navigation", "scene-gpu", "zoom", "label"} {
			set[name] = true
		}
	}
	content := []gosx.Node{}
	if set["island"] {
		content = append(content, r.RenderIsland("Counter", nil, gosx.El("p", gosx.Text("0"))))
	}
	if set["compute"] {
		if _, err := r.RegisterComputeIsland(ComputeIslandConfig{Name: "Counter"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"engine", "shared-engine"} {
		if set[name] {
			cfg := engine.Config{Name: "Example", Kind: engine.KindWorker}
			if name == "shared-engine" {
				cfg.Runtime = engine.RuntimeShared
			}
			content = append(content, r.RenderEngine(cfg, gosx.Text("")))
		}
	}
	if set["hub"] {
		r.BindHub("updates", "/gosx/hub/updates", []hydrate.HubBinding{})
	}
	if set["controller"] || set["controller-input"] {
		cfg := controller.Config{Name: "fixture"}
		if set["controller-input"] {
			cfg.Storage = &controller.Storage{}
		}
		r.RegisterController(cfg)
	}
	if set["text"] {
		content = append(content, gosx.El("p", gosx.Attrs(gosx.Attr("data-gosx-text-layout", "")), gosx.Text("Loader fixture text")))
	}
	opts := PerfAssetOptions{Navigation: set["navigation"], TextLayout: set["text"], RuntimeFetches: []PerfRuntimeFetch{}}
	props := map[string]any{}
	hasScene := false
	for _, name := range []string{"scene-gpu", "scene-webgl", "gpu-fallback", "gpu-only", "zoom", "label", "walk", "vessel", "particles", "compressed", "ktx", "model", "command", "timeline", "burst", "instance-stream", "animation", "late-text"} {
		hasScene = hasScene || set[name]
	}
	if hasScene {
		// Device-loss recovery is exercised while the scene is rendering.
		props["autoRotate"] = true
		opts.Backend, pair.GPU = "webgpu", "usable"
		if set["scene-webgl"] {
			opts.Backend, pair.GPU = "webgl2", "absent"
		}
		if set["gpu-fallback"] {
			opts.Backend, pair.GPU = "webgl2", "unusable"
		}
		gpu := pair.GPU != "absent"
		opts.NavigatorGPU = &gpu
		if set["gpu-only"] {
			props["backendCaps"] = map[string]any{"capable": []string{"webgpu"}}
		}
		if set["timeline"] {
			props["timelines"] = true
		}
		if set["burst"] {
			props["particleBursts"] = true
		}
		if set["zoom"] {
			props["controlZoom"] = true
		}
		if set["label"] {
			props["labels"] = []any{map[string]any{"id": "label", "text": "Fixture"}}
		}
		if set["walk"] {
			props["controls"], props["walk"] = "first-person", map[string]any{}
		}
		if set["vessel"] {
			props["vessel"] = map[string]any{"nodeId": "boat"}
		}
		if set["particles"] {
			props["computeParticles"] = []any{map[string]any{"count": 1}}
		}
		if set["compressed"] {
			props["compression"] = map[string]any{}
		}
		if set["ktx"] {
			props["objects"] = []any{map[string]any{"id": "box", "texture": "/fixture.ktx2"}}
		}
		if set["instance-stream"] {
			frame, err := (scene.InstanceStreamFrame{BatchID: "fixture", Revision: 1, Kind: scene.InstanceStreamTransform, Count: 0, Data: []float32{}}).Encode()
			if err != nil {
				t.Fatal(err)
			}
			pair.Stream = frame
		}
		if set["model"] {
			props["models"] = []any{map[string]any{"id": "model", "src": "/fixture.glb"}}
		}
		raw, err := json.Marshal(props)
		if err != nil {
			t.Fatal(err)
		}
		content = append(content, r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: raw}, gosx.Text("")))
		if pair.GPU == "usable" {
			pair.Triggers = append(pair.Triggers, "device-loss")
		}
		for _, name := range []string{"command", "timeline", "burst", "instance-stream", "animation", "late-text", "model"} {
			if !set[name] || name == "model" {
				continue
			}
			pair.Triggers = append(pair.Triggers, name)
			chunk := map[string]string{"command": "scene3d-command", "timeline": "scene3d-timeline", "burst": "scene3d-particle-burst", "instance-stream": "scene3d-instance-stream", "animation": "scene3d-animation", "late-text": "textlayout"}[name]
			if (variant == "monolith" || variant == "scene-monolith") && (name == "animation" || name == "late-text") {
				continue
			}
			for _, a := range r.perfAssets.Assets {
				if a.ID == "framework/runtime/bootstrap-feature-"+chunk+".js" {
					opts.RuntimeFetches = append(opts.RuntimeFetches, PerfRuntimeFetch{URL: base + a.URL, Phase: "after-ready"})
				}
			}
		}
	}
	if set["video-native"] || set["video-hls"] {
		props := json.RawMessage(`{"src":"/fixture.mp4","autoplay":false,"muted":true}`)
		if set["video-hls"] {
			props = json.RawMessage(`{"src":"/fixture.m3u8","autoplay":false,"muted":true}`)
		}
		content = append(content, r.RenderEngine(engine.Config{Name: "Video", Kind: engine.KindVideo, Props: props}, gosx.Text("")))
		if set["video-hls"] {
			for _, a := range r.perfAssets.Assets {
				if a.ID == "framework/runtime/hls.min.js" {
					opts.RuntimeFetches = append(opts.RuntimeFetches, PerfRuntimeFetch{URL: base + a.URL, Phase: "startup"})
				}
			}
		}
	}
	// Lite has no manifest asset URLs, and advertises no typography preload.
	// Its real forwarder requests the compatibility URL. The body is identical.
	if r.Summary().BootstrapMode == "lite" && opts.TextLayout {
		for i := range r.perfAssets.Assets {
			if r.perfAssets.Assets[i].ID == "framework/runtime/bootstrap-feature-textlayout.js" {
				r.perfAssets.Assets[i].URL = "/gosx/bootstrap-feature-textlayout.js"
			}
		}
	}
	uses, err := r.PerfAssetUses(opts)
	if err != nil {
		t.Fatalf("%s/%s/%v: %v", variant, base, features, err)
	}
	for _, a := range uses.Assets {
		switch a.Phase {
		case "startup":
			pair.Startup = append(pair.Startup, a.URL)
		case "after-ready":
			pair.AfterReady = append(pair.AfterReady, a.URL)
		case "dormant":
			pair.Dormant = append(pair.Dormant, a.URL)
		default:
			t.Fatal("unexpected runtime phase")
		}
		asset := loaderAsset{URL: a.URL, Source: sources[a.ID]}
		if asset.Source == "" {
			asset.Body = bodies[a.ID]
		}
		pair.Assets = append(pair.Assets, asset)
	}
	sort.Strings(pair.Startup)
	sort.Strings(pair.AfterReady)
	sort.Strings(pair.Dormant)
	head := gosx.RenderHTML(gosx.Fragment(r.PreloadHints(), r.PageHead()))
	if opts.Navigation {
		head = `<script data-gosx-navigation="true" defer crossorigin="anonymous" referrerpolicy="no-referrer" src="` + navigationLoaderPath(r) + `"></script>` + head
	}
	document := map[string]any{"base": base, "head": head, "body": gosx.RenderHTML(gosx.Fragment(content...)), "runtime": r.Summary(), "navigation": opts.Navigation}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), pair
}

func navigationLoaderPath(r *Renderer) string {
	for _, a := range r.perfAssets.Assets {
		if a.ID == "framework/runtime/navigation.js" {
			return a.URL
		}
	}
	return ""
}

func perfLoaderBodies(t *testing.T, m *buildmanifest.Manifest) (map[string][]byte, map[string]string) {
	t.Helper()
	bodies, sources, replacements := map[string][]byte{}, map[string]string{}, map[string]buildmanifest.HashedAsset{}
	for i := range m.PerfAssetUses.Assets {
		a := &m.PerfAssetUses.Assets[i]
		name := strings.TrimPrefix(a.ID, "framework/runtime/")
		body := []byte("\x00asm" + name)
		source := ""
		if a.Kind == "js" && name != "wasm_exec.js" && name != "standard-go-wasm_exec.js" {
			source = name
			if name == "hls.min.js" {
				source = "vendor/hls.min.js"
			}
			if name == "navigation.js" {
				source = "../runtime/host/navigation-runtime.min.js"
			}
			var err error
			body, err = os.ReadFile(filepath.Join("..", "client", "js", source))
			if err != nil {
				t.Fatal(err)
			}
		} else if a.Kind == "program" {
			body = []byte(`{"name":"Counter"}`)
		}
		if a.Kind == "js" && source == "" {
			body = []byte("// Native VM shim boundary.\n")
		}
		bodies[a.ID], sources[a.ID] = body, source
		sha := sha256.Sum256(body)
		oldFile := filepath.Base(a.URL)
		ext := filepath.Ext(oldFile)
		if a.Kind == "program" {
			ext = ".json"
		}
		stem := strings.Split(oldFile, ".")[0]
		file := stem + "." + buildmanifest.ContentHash(body) + ext
		replacements[oldFile] = buildmanifest.HashedAsset{File: file, Hash: buildmanifest.ContentHash(body), Size: int64(len(body))}
		a.SHA256, a.URL = hex.EncodeToString(sha[:]), strings.TrimSuffix(a.URL, oldFile)+file
		if name == "navigation.js" {
			a.URL = runtimehost.NavigationRuntimePath
		}
	}
	runtime := reflect.ValueOf(&m.Runtime).Elem()
	for i := 0; i < runtime.NumField(); i++ {
		field := runtime.Field(i)
		if field.Type() == reflect.TypeFor[buildmanifest.HashedAsset]() {
			a := field.Interface().(buildmanifest.HashedAsset)
			if replacement, ok := replacements[a.File]; ok {
				field.Set(reflect.ValueOf(replacement))
			}
		}
	}
	for name, variant := range m.Runtime.WASMVariants {
		variant.HashedAsset = replacements[variant.File]
		m.Runtime.WASMVariants[name] = variant
	}
	for i := range m.Islands {
		m.Islands[i].HashedAsset, m.Islands[i].Format = replacements[m.Islands[i].File], "json"
	}
	return bodies, sources
}
