package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
)

func perfGraphBuildFixture(t *testing.T) (string, *BuildManifest) {
	t.Helper()
	dist := t.TempDir()
	for _, name := range []string{"runtime", "islands", "css"} {
		if err := os.MkdirAll(filepath.Join(dist, "assets", name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(dir, name, ext string, raw []byte) HashedAsset {
		a, err := writeHashed(filepath.Join(dist, "assets", dir), name, ext, raw)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	m := &BuildManifest{}
	m.Runtime.WASMOptimization = map[string]buildmanifest.WASMOptimization{}
	m.Runtime.WASMVariants = map[string]buildmanifest.RuntimeVariantAsset{}
	for _, name := range []string{"core", "engine", "collab", "full", "islands"} {
		// Synthetic emitted bodies exercise identity and evidence binding,
		// not a compiler or a real optimizer measurement.
		raw := append([]byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, []byte(name)...)
		a := write("runtime", "runtime-"+name, ".wasm", raw)
		m.Runtime.WASMOptimization[name] = buildmanifest.WASMOptimization{Tool: "wasm-opt", Version: "108", Applied: true,
			InputSHA256: perfBuildDigest(raw), OutputSHA256: perfBuildDigest(raw)}
		if name == "islands" {
			m.Runtime.WASMIslands = a
		} else {
			m.Runtime.WASMVariants[name] = buildmanifest.RuntimeVariantAsset{HashedAsset: a, Variant: name}
		}
		if name == "full" {
			m.Runtime.WASM = a
		}
	}
	m.Runtime.Bootstrap = write("runtime", "bootstrap", ".js", []byte("/* monolith */"))
	m.Runtime.BootstrapRuntime = write("runtime", "bootstrap-runtime", ".js", []byte("/* selected loader */"))
	m.Runtime.BootstrapFeatureScene3DWebGPU = write("runtime", "webgpu", ".js", []byte("/* webgpu */"))
	m.Runtime.BootstrapFeatureScene3DWebGL = write("runtime", "webgl", ".js", []byte("/* webgl */"))
	m.Runtime.VideoHLS = write("runtime", "hls", ".js", []byte("/* hls */"))
	m.Islands = []IslandAsset{{Name: "counter", Format: "bin", HashedAsset: write("islands", "counter", ".gxi", []byte("program"))}}
	m.CSS = []CSSAsset{{Component: "counter", Source: "app/counter.css", HashedAsset: write("css", "counter", ".css", []byte(".counter{}"))}}
	return dist, m
}

func TestPerfGraphBuildInventory(t *testing.T) {
	dist, m := perfGraphBuildFixture(t)
	if err := stagePerfBuildAssets(dist, m, "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := m.ValidatePerfAssetUses(); err != nil {
		t.Fatal(err)
	}
	seen, ids := map[string]buildmanifest.PerfAssetUse{}, []string{}
	for _, a := range m.PerfAssetUses.Assets {
		if _, ok := seen[a.ID]; ok {
			t.Fatalf("duplicate physical compatibility alias: %s", a.ID)
		}
		seen[a.ID], ids = a, append(ids, a.ID)
		if a.Phase != "dormant" || a.Dependencies == nil {
			t.Fatalf("unproven startup use: %+v", a)
		}
	}
	if !sort.StringsAreSorted(ids) || len(ids) != 13 {
		t.Fatalf("inventory: %v", ids)
	}
	for _, name := range []string{"core", "engine", "collab", "full", "islands"} {
		if a := seen["framework/runtime/"+name+".wasm"]; a.Owner != "framework" || a.Kind != "wasm" {
			t.Fatalf("missing capability artifact %s", name)
		}
	}
	for name, condition := range map[string]string{"bootstrap-feature-scene3d-webgpu.js": "webgpu", "bootstrap-feature-scene3d-webgl.js": "webgl", "hls.min.js": "hls-required"} {
		if a := seen["framework/runtime/"+name]; a.Condition != condition {
			t.Fatalf("condition %s: %+v", name, a)
		}
	}
	nav := seen["framework/runtime/navigation.js"]
	if nav.URL != runtimehost.NavigationRuntimePath || nav.SHA256 != perfBuildDigest([]byte(runtimehost.NavigationRuntime)) {
		t.Fatal("external navigation identity is not bound to the served body")
	}
	for suffix, want := range map[string][]byte{"": []byte(runtimehost.NavigationRuntime), ".gz": runtimehost.NavigationRuntimeGzip, ".br": runtimehost.NavigationRuntimeBrotli} {
		got, err := os.ReadFile(filepath.Join(dist, "assets/runtime", filepath.Base(nav.URL)+suffix))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("revision-specific navigation representation missing", err)
		}
	}
	for id, kind := range map[string]string{"app/fixture/islands/counter": "program", "app/fixture/css/app/counter.css": "css"} {
		if a := seen[id]; a.Owner != "app" || a.Kind != kind {
			t.Fatalf("app ownership %s: %+v", id, a)
		}
	}
	before, _ := json.Marshal(m.PerfAssetUses)
	if err := stagePerfBuildAssets(dist, m, "fixture"); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(m.PerfAssetUses)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("producer is not deterministic")
	}
	if _, err := writeBuildManifest(dist, m); err != nil {
		t.Fatal(err)
	}
	loaded, err := buildmanifest.Load(filepath.Join(dist, "build.json"))
	if err != nil || !reflect.DeepEqual(loaded.PerfAssetUses, m.PerfAssetUses) {
		t.Fatalf("manifest round trip: %v", err)
	}
}

func TestPerfGraphBuildRejectsUnverifiedBodies(t *testing.T) {
	for _, name := range []string{"missing-optimizer", "failed-optimizer", "wrong-version", "wrong-output", "missing-body", "bad-sidecar", "unsafe-name", "duplicate-app-id", "wrong-filename-hash", "escaping-file"} {
		t.Run(name, func(t *testing.T) {
			dist, m := perfGraphBuildFixture(t)
			proof := m.Runtime.WASMOptimization["core"]
			switch name {
			case "missing-optimizer":
				delete(m.Runtime.WASMOptimization, "core")
			case "failed-optimizer":
				proof.Applied = false
			case "wrong-version":
				proof.Version = "109"
			case "wrong-output":
				proof.OutputSHA256 = strings.Repeat("0", 64)
			case "missing-body":
				if err := os.Remove(filepath.Join(dist, "assets", "runtime", m.Runtime.Bootstrap.File)); err != nil {
					t.Fatal(err)
				}
			case "bad-sidecar":
				if err := os.WriteFile(filepath.Join(dist, "assets", "runtime", m.Runtime.Bootstrap.File+".br"), []byte("wrong"), 0644); err != nil {
					t.Fatal(err)
				}
			case "unsafe-name":
				m.CSS[0].Source = "private?name"
			case "wrong-filename-hash":
				m.Runtime.Bootstrap.File = "bootstrap.0000000000000000.js"
				if err := os.WriteFile(filepath.Join(dist, "assets", "runtime", m.Runtime.Bootstrap.File), []byte("wrong hash"), 0644); err != nil {
					t.Fatal(err)
				}
			case "escaping-file":
				m.Runtime.Bootstrap.File = "../private.txt"
			case "duplicate-app-id":
				m.CSS = append(m.CSS, CSSAsset{Component: "other", Source: m.CSS[0].Source, HashedAsset: m.Runtime.Bootstrap})
				if err := os.WriteFile(filepath.Join(dist, "assets", "css", m.Runtime.Bootstrap.File), []byte("/* monolith */"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if name != "missing-optimizer" {
				m.Runtime.WASMOptimization["core"] = proof
			}
			err := stagePerfBuildAssets(dist, m, "fixture")
			var typed *buildmanifest.PerfAssetError
			if !errors.As(err, &typed) || typed.Code == "" || typed.Pointer == "" || strings.Contains(err.Error(), "private?name") {
				t.Fatalf("expected safe typed error: %v", err)
			}
			if m.PerfAssetUses != nil {
				t.Fatal("failed producer published a partial graph")
			}
		})
	}
}

func TestPerfGraphBuildOptions(t *testing.T) {
	for _, id := range []string{"Fixture", "../private", "fixture?query", "x\xff", strings.Repeat("a", 49)} {
		if err := validatePerfBuildOptions(BuildOptions{PerfAppID: id}); err == nil {
			t.Fatalf("accepted ID %q", id)
		}
	}
	if err := RunBuildWithOptions("unused", BuildOptions{PerfAppID: "fixture", Dev: true}); err == nil {
		t.Fatal("dev build accepted canonical producer before side-effect barrier")
	}
	if err := validatePerfBuildOptions(BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := validatePerfBuildOptions(BuildOptions{PerfAppID: "fixture"}); err != nil {
		t.Fatal(err)
	}
}

func TestPerfGraphOptimizerRecordsSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.wasm")
	before := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}
	after := append(append([]byte{}, before...), 0, 0)
	if err := os.WriteFile(path, before, 0644); err != nil {
		t.Fatal(err)
	}
	version := func() ([]byte, error) { return []byte("wasm-opt version 108 (version_108)\n"), nil }
	proof, err := recordPerfWASMOptimization(path, version, func(p string) (bool, error) { return true, os.WriteFile(p, after, 0644) })
	if err != nil || proof == nil || !proof.Applied || proof.Version != "108" || proof.InputSHA256 != perfBuildDigest(before) || proof.OutputSHA256 != perfBuildDigest(after) {
		t.Fatalf("success proof: %+v, %v", proof, err)
	}
	for _, name := range []string{"missing-version", "wrong-version", "skipped", "failed", "invalid-output", "invalid-input"} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, before, 0644); err != nil {
				t.Fatal(err)
			}
			v := version
			called := false
			apply := func(p string) (bool, error) {
				called = true
				switch name {
				case "skipped":
					return false, nil
				case "failed":
					return true, errors.New("private optimizer detail")
				case "invalid-output":
					return true, os.WriteFile(p, nil, 0644)
				}
				return true, nil
			}
			switch name {
			case "missing-version":
				v = func() ([]byte, error) { return nil, errors.New("private version detail") }
			case "wrong-version":
				v = func() ([]byte, error) { return []byte("wasm-opt version 109 (version_109)"), nil }
			case "invalid-input":
				if err := os.WriteFile(path, []byte("wrong"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			proof, err := recordPerfWASMOptimization(path, v, apply)
			if proof != nil || err == nil || err.Error() != "optimizer-unverified at /runtime/wasmOptimization" {
				t.Fatalf("unverified proof: %+v, %v", proof, err)
			}
			if (name == "missing-version" || name == "wrong-version" || name == "invalid-input") && called {
				t.Fatal("optimizer ran without valid inputs")
			}
		})
	}
}
