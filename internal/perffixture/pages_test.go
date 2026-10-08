package perffixture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	runtimewasm "m31labs.dev/gosx/client/runtime/wasm"
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
			classes, err := pagecaps.Classify(caps, strings.HasPrefix(tc.shape, "game-"))
			if err != nil || !reflect.DeepEqual(classes, tc.classes) {
				t.Fatalf("classes %v, want %v: %v", classes, tc.classes, err)
			}
			if caps.Runtime != tc.runtime || caps.BootstrapMode != tc.bootstrap || caps.Islands != tc.islands || caps.ComputeIslands != tc.compute || caps.Engines != tc.engines {
				t.Fatalf("rendered capabilities: %+v", caps)
			}
			text := string(html)
			if !strings.HasPrefix(text, "<!doctype html>") || !strings.Contains(text, "width=device-width, initial-scale=1") {
				t.Fatal("incomplete production document")
			}
			if tc.shape == "static" {
				if strings.Contains(text, "<script") || caps.Bootstrap || caps.WASM {
					t.Fatal("static page acquired framework execution")
				}
			} else if !strings.Contains(text, `src="`+runtimehost.NavigationRuntimePath+`"`) || !strings.Contains(text, "defer") {
				t.Fatal("external deferred navigation is missing")
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
	for _, shape := range []string{"engine-js", "engine-shared", "go-wasm", "scene-js", "scene-shared", "video"} {
		t.Run(shape, func(t *testing.T) {
			r := pageRenderer(t, manifest, "configured")
			html, err := Page(r, shape, pageAssets())
			if err != nil {
				t.Fatal(err)
			}
			engines := r.Manifest().Engines
			if len(engines) != 1 {
				t.Fatal("actual engine declaration missing")
			}
			entry := engines[0]
			switch shape {
			case "engine-js":
				if !strings.Contains(string(html), `src="/assets/engine.js"`) || entry.Component != "BudgetEngine" {
					t.Fatal("external app factory missing")
				}
			case "engine-shared", "go-wasm", "scene-shared":
				want := pageAssets().EngineSharedURL
				if shape == "go-wasm" {
					want = pageAssets().GoWASMURL
				}
				if entry.ProgramRef != want {
					t.Fatal("app-owned program reference missing")
				}
			case "scene-js":
				if entry.Component != "GoSXScene3D" || len(entry.Props) == 0 || !strings.Contains(string(entry.Props), "box") {
					t.Fatal("real scene graph missing")
				}
			case "video":
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
		})
	}
	encoded, _ := json.Marshal(pageAssets())
	if string(encoded) != "{}" {
		t.Fatal("native asset options entered a public record")
	}
}

func TestPerfFixturePagesRejectMissingAndUnsafeInputs(t *testing.T) {
	manifest := pageManifest(t)
	for _, shape := range []string{"engine-js", "engine-shared", "go-wasm", "scene-shared", "video"} {
		if _, err := Page(pageRenderer(t, manifest, "configured"), shape, Assets{}); err == nil {
			t.Fatalf("missing program/media accepted for %s", shape)
		}
	}
	for _, value := range []string{"//private.example/file", "private/file", "/a/../secret", "/a//file", "/a?secret=1", "/a#secret", "/a\\secret", "/a\x00secret"} {
		assets := Assets{EngineJSURL: value, EngineSharedURL: value, GoWASMURL: value, VideoURL: value}
		for _, shape := range []string{"engine-js", "engine-shared", "go-wasm", "scene-shared", "video"} {
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
