package perffixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"m31labs.dev/gosx/buildmanifest"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	runtimewasm "m31labs.dev/gosx/client/runtime/wasm"
	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/internal/pagecaps"
	"m31labs.dev/gosx/island"
)

func pageManifest(t *testing.T) *buildmanifest.Manifest {
	t.Helper()
	m := &buildmanifest.Manifest{PerfAssetUses: &buildmanifest.PerfAssetUses{Version: 1, Assets: []buildmanifest.PerfAssetUse{}}}
	// Synthetic bodies exercise rendered contracts, not production byte limits.
	add := func(name, bucket, kind, owner string) buildmanifest.HashedAsset {
		raw := []byte(name)
		sha := sha256.Sum256(raw)
		hash := buildmanifest.ContentHash(raw)
		a := buildmanifest.HashedAsset{File: name + "." + hash + "." + kind, Hash: hash, Size: int64(len(raw))}
		id := "framework/runtime/" + name + "." + kind
		if owner == "app" {
			id = "app/fixture/islands/" + name
		}
		m.PerfAssetUses.Assets = append(m.PerfAssetUses.Assets, buildmanifest.PerfAssetUse{
			ID: id, SHA256: hex.EncodeToString(sha[:]), URL: buildmanifest.AssetURL("/gosx/assets", bucket, a.File),
			Owner: owner, Kind: kind, Phase: "dormant", Condition: "always", Dependencies: []string{},
		})
		return a
	}
	for _, entry := range []struct {
		name string
		out  *buildmanifest.HashedAsset
	}{
		{"bootstrap", &m.Runtime.Bootstrap}, {"bootstrap-lite", &m.Runtime.BootstrapLite}, {"bootstrap-runtime", &m.Runtime.BootstrapRuntime},
		{"wasm_exec", &m.Runtime.WASMExec}, {"standard-go-wasm_exec", &m.Runtime.StandardGoWASMExec}, {"patch", &m.Runtime.Patch}, {"relay", &m.Runtime.Relay},
		{"bootstrap-feature-islands", &m.Runtime.BootstrapFeatureIslands}, {"bootstrap-feature-engines", &m.Runtime.BootstrapFeatureEngines},
		{"bootstrap-feature-scene3d", &m.Runtime.BootstrapFeatureScene3D}, {"bootstrap-feature-scene3d-hydrate", &m.Runtime.BootstrapFeatureScene3DHydrate},
		{"bootstrap-feature-scene3d-webgpu", &m.Runtime.BootstrapFeatureScene3DWebGPU}, {"bootstrap-feature-scene3d-webgl", &m.Runtime.BootstrapFeatureScene3DWebGL},
		{"hls.min", &m.Runtime.VideoHLS},
	} {
		*entry.out = add(entry.name, "runtime", "js", "framework")
	}
	m.Runtime.WASMVariants = map[string]buildmanifest.RuntimeVariantAsset{}
	for _, name := range []string{"core", "engine", "collab", "full"} {
		a := add(name, "runtime", "wasm", "framework")
		m.Runtime.WASMVariants[name] = buildmanifest.RuntimeVariantAsset{HashedAsset: a, Variant: name,
			FeatureMask: uint32(runtimewasm.RequiredFeaturesForVariant(runtimewasm.Variant(name)))}
	}
	m.Runtime.WASM = m.Runtime.WASMVariants["full"].HashedAsset
	m.Runtime.WASMIslands = add("islands", "runtime", "wasm", "framework")
	m.Islands = []buildmanifest.IslandAsset{{Name: "Counter", Format: "bin", HashedAsset: add("Counter", "islands", "program", "app")}}
	return m
}

func pageRenderer(t *testing.T, manifest *buildmanifest.Manifest, mode string) *island.Renderer {
	t.Helper()
	r, err := island.NewPerfFixtureRenderer(manifest, mode)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func pageAssets() Assets {
	return Assets{EngineJSURL: "/assets/engine.js", EngineSharedURL: "/assets/engine.gxi", GoWASMURL: "/assets/engine.wasm", VideoURL: "/assets/video.mp4"}
}

type renderedPage struct {
	manifest hydrate.Manifest
	islands  map[string]map[string]string
	engines  map[string]map[string]string
	scripts  []map[string]string
}

func readRenderedPage(t *testing.T, data []byte) renderedPage {
	t.Helper()
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	page := renderedPage{islands: map[string]map[string]string{}, engines: map[string]map[string]string{}}
	manifestSeen := false
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			if node.Data == "template" {
				return
			}
			attrs := map[string]string{}
			for _, attr := range node.Attr {
				attrs[attr.Key] = attr.Val
			}
			if _, ok := attrs["data-gosx-island"]; ok {
				page.islands[attrs["id"]] = attrs
			}
			if _, ok := attrs["data-gosx-engine"]; ok {
				page.engines[attrs["id"]] = attrs
			}
			if node.Data == "script" {
				page.scripts = append(page.scripts, attrs)
				if attrs["id"] == "gosx-manifest" {
					if manifestSeen || attrs["type"] != "application/json" {
						t.Fatal("invalid rendered hydration manifest")
					}
					manifestSeen = true
					var raw strings.Builder
					for child := node.FirstChild; child != nil; child = child.NextSibling {
						raw.WriteString(child.Data)
					}
					if err := json.Unmarshal([]byte(raw.String()), &page.manifest); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
	return page
}

func (p renderedPage) declaresGame() bool {
	for _, attrs := range p.engines {
		if attrs["data-gosx-game"] == "true" {
			return true
		}
	}
	return false
}

func (p renderedPage) hasDeferredScript(src string) bool {
	for _, script := range p.scripts {
		if script["src"] == src {
			_, ok := script["defer"]
			return ok
		}
	}
	return false
}

func TestPerfFixturePagesCoverActualRenderedTypes(t *testing.T) {
	for _, tc := range []struct {
		shape, mode, runtime, bootstrap string
		classes                         []string
		islands, compute, engines       int
	}{
		{"static", "configured", "none", "none", []string{"static"}, 0, 0, 0},
		{"enhanced", "configured", "js", "none", []string{"enhanced"}, 0, 0, 0},
		{"enhanced-lite", "configured", "js", "lite", []string{"enhanced"}, 0, 0, 0},
		{"island", "configured", "shared", "full", []string{"island"}, 1, 0, 0},
		{"compute-island", "configured", "shared", "full", []string{"island"}, 0, 1, 0},
		{"engine-js", "configured", "js", "full", []string{"engine/js"}, 0, 0, 1},
		{"engine-shared", "configured", "shared", "full", []string{"engine/shared"}, 0, 0, 1},
		{"go-wasm", "configured", "go-wasm", "full", []string{"go-wasm"}, 0, 0, 1},
		{"scene-js", "configured", "js", "full", []string{"scene3d/js"}, 0, 0, 1},
		{"scene-shared", "configured", "shared", "full", []string{"scene3d/shared"}, 0, 0, 1},
		{"game-js", "configured", "js", "full", []string{"game/js", "scene3d/js"}, 0, 0, 1},
		{"game-shared", "configured", "shared", "full", []string{"game/shared", "scene3d/shared"}, 0, 0, 1},
		{"mixed", "configured", "mixed", "full", []string{"island", "scene3d/js"}, 1, 0, 1},
		{"video", "configured", "js", "full", []string{"video"}, 0, 0, 1},
		{"preview", "preview", "shared", "preview", []string{"preview"}, 0, 0, 0},
		{"lite-missing", "lite-missing", "js", "lite", []string{"enhanced"}, 0, 0, 0},
		{"selective-missing", "selective-missing", "shared", "full", []string{"island"}, 1, 0, 0},
		{"full-unconfigured", "full-unconfigured", "shared", "full", []string{"island"}, 1, 0, 0},
	} {
		t.Run(tc.shape, func(t *testing.T) {
			manifest := pageManifest(t)
			before, _ := json.Marshal(manifest)
			r := pageRenderer(t, manifest, tc.mode)
			html, err := Page(r, tc.shape, pageAssets())
			if err != nil {
				t.Fatal(err)
			}
			caps, err := pagecaps.FromHTML(html)
			if err != nil {
				t.Fatal(err)
			}
			page := readRenderedPage(t, html)
			classes, err := pagecaps.Classify(caps, page.declaresGame())
			if err != nil || !reflect.DeepEqual(classes, tc.classes) {
				t.Fatalf("classes %v, want %v: %v", classes, tc.classes, err)
			}
			if caps.Runtime != tc.runtime || caps.BootstrapMode != tc.bootstrap || caps.Islands != tc.islands || caps.ComputeIslands != tc.compute || caps.Engines != tc.engines {
				t.Fatalf("rendered capabilities: %+v", caps)
			}
			if len(page.islands) != tc.islands || len(page.engines) != tc.engines {
				t.Fatal("rendered DOM mounts differ from the hydration contract")
			}
			wantProgram := buildmanifest.AssetURL("/gosx/assets", "islands", manifest.Islands[0].File)
			for _, entry := range page.manifest.Islands {
				if page.islands[entry.ID]["data-gosx-island"] != "Counter" || entry.Component != "Counter" || entry.ProgramRef != wantProgram || entry.ProgramFormat != "bin" {
					t.Fatal("rendered island declaration lost its mount or program")
				}
			}
			for _, entry := range page.manifest.ComputeIslands {
				if entry.Component != "Counter" || entry.ProgramRef != wantProgram || entry.ProgramFormat != "bin" || len(page.islands) != 0 {
					t.Fatal("rendered compute declaration lost its headless program")
				}
			}
			for _, entry := range page.manifest.Engines {
				attrs := page.engines[entry.MountID]
				if attrs["data-gosx-engine"] != entry.Component || attrs["data-gosx-engine-id"] != entry.ID || attrs["data-gosx-engine-kind"] != entry.Kind {
					t.Fatal("rendered engine mount differs from its declaration")
				}
			}
			if tc.mode == "full-unconfigured" {
				full := manifest.Runtime.WASM
				if page.manifest.Runtime.Variant != "full" || page.manifest.Runtime.Path != buildmanifest.AssetURL("/gosx/assets", "runtime", full.File) || page.manifest.Runtime.Hash != full.Hash {
					t.Fatal("rendered compatibility declaration lost the full WASM fallback")
				}
			}
			bootstrap, navigation, relay := 0, 0, 0
			for _, script := range page.scripts {
				if script["src"] == runtimehost.NavigationRuntimePath {
					navigation++
					if _, ok := script["defer"]; !ok {
						t.Fatal("navigation is not deferred")
					}
				}
				if script["data-gosx-script"] == "relay" {
					relay++
					if script["src"] != "/gosx/relay.js" || !page.hasDeferredScript(script["src"]) {
						t.Fatal("rendered preview relay lost its compatibility declaration")
					}
				}
				if script["data-gosx-script"] == "bootstrap" {
					bootstrap++
					want := manifest.Runtime.BootstrapRuntime
					if tc.bootstrap == "lite" {
						want = manifest.Runtime.BootstrapLite
					}
					if tc.mode != "configured" {
						want = manifest.Runtime.Bootstrap
					}
					if script["data-gosx-bootstrap-mode"] != tc.bootstrap || script["src"] != buildmanifest.AssetURL("/gosx/assets", "runtime", want.File) {
						t.Fatal("rendered bootstrap selection differs from its declared mode")
					}
					if _, ok := script["defer"]; !ok {
						t.Fatal("bootstrap is not deferred")
					}
				}
			}
			if (bootstrap == 1) != (tc.bootstrap != "none") || bootstrap > 1 || (navigation == 1) != (tc.shape != "static") || navigation > 1 || (relay == 1) != (tc.mode == "preview") || relay > 1 {
				t.Fatal("rendered bootstrap, navigation, or preview declaration missing")
			}
			text := string(html)
			if !strings.HasPrefix(text, "<!doctype html>") || !strings.Contains(text, "width=device-width, initial-scale=1") {
				t.Fatal("incomplete production document")
			}
			if tc.shape == "static" {
				if strings.Contains(text, "<script") || caps.Bootstrap || caps.WASM {
					t.Fatal("static page acquired framework execution")
				}
			}
			after, _ := json.Marshal(manifest)
			if string(before) != string(after) {
				t.Fatal("page mutated the supplied build inventory")
			}
			fresh := pageRenderer(t, manifest, tc.mode)
			again, err := Page(fresh, tc.shape, pageAssets())
			if err != nil || string(again) != text {
				t.Fatal("fresh visit changed mount IDs or declarations", err)
			}
		})
	}
}

func TestPerfFixturePagesKeepRealProgramsScenesAndMutedMedia(t *testing.T) {
	manifest := pageManifest(t)
	for _, shape := range []string{"engine-js", "engine-shared", "go-wasm", "scene-js", "scene-shared", "game-js", "game-shared", "mixed", "video"} {
		t.Run(shape, func(t *testing.T) {
			r := pageRenderer(t, manifest, "configured")
			html, err := Page(r, shape, pageAssets())
			if err != nil {
				t.Fatal(err)
			}
			page := readRenderedPage(t, html)
			engines := page.manifest.Engines
			if len(engines) != 1 {
				t.Fatal("actual engine declaration missing")
			}
			entry := engines[0]
			if shape == "engine-js" || shape == "engine-shared" || shape == "go-wasm" {
				if entry.Component != "BudgetEngine" || entry.Kind != "surface" {
					t.Fatal("rendered app engine lost its surface declaration")
				}
			}
			switch shape {
			case "engine-js":
				if !page.hasDeferredScript(pageAssets().EngineJSURL) || entry.Runtime != "" || entry.ProgramRef != "" {
					t.Fatal("external app factory missing")
				}
			case "engine-shared", "go-wasm", "scene-shared", "game-shared":
				want := pageAssets().EngineSharedURL
				if shape == "go-wasm" {
					want = pageAssets().GoWASMURL
				}
				if entry.ProgramRef != want {
					t.Fatal("app-owned program reference missing")
				}
				wantRuntime := "shared"
				if shape == "go-wasm" {
					wantRuntime = "go-wasm"
				}
				if entry.Runtime != wantRuntime {
					t.Fatal("rendered program lost its runtime")
				}
				if shape == "engine-shared" && !page.hasDeferredScript(pageAssets().EngineJSURL) {
					t.Fatal("shared engine app factory missing")
				}
			case "video":
				if entry.Component != "GoSXVideo" || entry.Kind != "video" || !slices.Contains(entry.Capabilities, "video") {
					t.Fatal("rendered media lost its managed-video declaration")
				}
				var props struct {
					Src      string  `json:"src"`
					Muted    bool    `json:"muted"`
					Volume   float64 `json:"volume"`
					Autoplay bool    `json:"autoplay"`
				}
				if err := json.Unmarshal(entry.Props, &props); err != nil || !props.Muted || props.Volume != 0 || props.Autoplay || props.Src != pageAssets().VideoURL {
					t.Fatal("video fixture can start audible playback", err)
				}
			}
			if strings.HasPrefix(shape, "scene-") || strings.HasPrefix(shape, "game-") || shape == "mixed" {
				attrs := page.engines[entry.MountID]
				var props struct {
					Scene            json.RawMessage `json:"scene"`
					GameProfile      string          `json:"gameProfile"`
					FixedStepSeconds float64         `json:"fixedStepSeconds"`
				}
				if err := json.Unmarshal(entry.Props, &props); err != nil || entry.Component != "GoSXScene3D" || attrs["data-gosx-scene3d"] != "true" || !strings.Contains(string(props.Scene), `"box"`) {
					t.Fatal("rendered Scene3D declaration lost its graph", err)
				}
				if strings.HasSuffix(shape, "-js") && (entry.Runtime != "" || entry.ProgramRef != "") {
					t.Fatal("JavaScript scene acquired a shared program")
				}
				if strings.HasPrefix(shape, "game-") {
					profile := game.InteractiveProfile()
					step := time.Second / 60
					if attrs["data-gosx-game"] != "true" || attrs["data-gosx-game-profile"] != "interactive" || attrs["data-gosx-game-fixed-step"] != step.String() || props.GameProfile != "interactive" || props.FixedStepSeconds != step.Seconds() {
						t.Fatal("rendered game profile or fixed-step contract missing")
					}
					for _, capability := range profile.Capabilities {
						if !slices.Contains(entry.Capabilities, string(capability)) || !slices.Contains(strings.Fields(attrs["data-gosx-engine-capabilities"]), string(capability)) {
							t.Fatalf("rendered game capability %s missing", capability)
						}
					}
					for _, capability := range profile.RequiredCapabilities {
						if !slices.Contains(entry.RequiredCapabilities, string(capability)) || !slices.Contains(strings.Fields(attrs["data-gosx-engine-required-capabilities"]), string(capability)) {
							t.Fatalf("rendered game requirement %s missing", capability)
						}
					}
				} else if page.declaresGame() || props.GameProfile != "" || props.FixedStepSeconds != 0 {
					t.Fatal("ordinary scene was declared as a game")
				}
			}
		})
	}
	encoded, _ := json.Marshal(pageAssets())
	if string(encoded) != "{}" {
		t.Fatal("native asset options entered a public record")
	}
}

func TestPerfFixturePagesRejectMissingAndUnsafeInputs(t *testing.T) {
	manifest := pageManifest(t)
	assets := pageAssets()
	assets.EngineJSURL = ""
	if _, err := Page(pageRenderer(t, manifest, "configured"), "engine-shared", assets); err == nil {
		t.Fatal("shared engine accepted without its app factory")
	}
	for _, shape := range []string{"engine-js", "engine-shared", "go-wasm", "scene-shared", "game-shared", "video"} {
		if _, err := Page(pageRenderer(t, manifest, "configured"), shape, Assets{}); err == nil {
			t.Fatalf("missing program/media accepted for %s", shape)
		}
	}
	for _, value := range []string{"//private.example/file", "private/file", "/a/../secret", "/a//file", "/a?secret=1", "/a#secret", "/a\\secret", "/a\x00secret"} {
		assets := Assets{EngineJSURL: value, EngineSharedURL: value, GoWASMURL: value, VideoURL: value}
		for _, shape := range []string{"engine-js", "engine-shared", "go-wasm", "scene-shared", "game-shared", "video"} {
			_, err := Page(pageRenderer(t, manifest, "configured"), shape, assets)
			var typed *buildmanifest.PerfAssetError
			if !errors.As(err, &typed) || typed.Code != "invalid-input" || !strings.HasPrefix(typed.Pointer, "/fixture/assets/") || strings.Contains(err.Error(), value) {
				t.Fatal("unsafe URL accepted or echoed", err)
			}
		}
	}
	if _, err := Page(nil, "static", Assets{}); err == nil {
		t.Fatal("nil renderer accepted")
	}
	if _, err := Page(pageRenderer(t, manifest, "configured"), "private-shape", Assets{}); err == nil || strings.Contains(err.Error(), "private-shape") {
		t.Fatal("unknown shape accepted or echoed", err)
	}
	if _, err := Page(pageRenderer(t, manifest, "configured"), "preview", Assets{}); err == nil {
		t.Fatal("preview page accepted without its compatibility mode")
	}
}
