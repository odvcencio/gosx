package island

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/hydrate"
)

func TestRuntimeRefPrefersCompressedTransferCost(t *testing.T) {
	for _, tc := range []struct {
		name               string
		candidate, current hydrate.RuntimeRef
		want               bool
	}{
		{"raw tie prefers Brotli", hydrate.RuntimeRef{Path: "/z.wasm", Size: 666700, BrotliSize: 219517}, hydrate.RuntimeRef{Path: "/a.wasm", Size: 666700, BrotliSize: 219927}, true},
		{"Brotli precedes raw", hydrate.RuntimeRef{Path: "/z.wasm", Size: 666700, BrotliSize: 219517}, hydrate.RuntimeRef{Path: "/a.wasm", Size: 666696, BrotliSize: 219927}, true},
		{"Brotli precedes gzip", hydrate.RuntimeRef{Path: "/z.wasm", Size: 200, GzipSize: 100, BrotliSize: 50}, hydrate.RuntimeRef{Path: "/a.wasm", Size: 100, GzipSize: 80, BrotliSize: 60}, true},
		{"gzip when Brotli missing", hydrate.RuntimeRef{Path: "/z.wasm", Size: 200, GzipSize: 70}, hydrate.RuntimeRef{Path: "/a.wasm", Size: 100, GzipSize: 80}, true},
		{"common gzip with partial Brotli", hydrate.RuntimeRef{Path: "/z.wasm", Size: 200, GzipSize: 70, BrotliSize: 50}, hydrate.RuntimeRef{Path: "/a.wasm", Size: 100, GzipSize: 80}, true},
		{"no compressed metadata uses raw", hydrate.RuntimeRef{Path: "/z.wasm", Size: 90}, hydrate.RuntimeRef{Path: "/a.wasm", Size: 100}, true},
		{"missing compressed metadata uses raw cost", hydrate.RuntimeRef{Path: "/a.wasm", Size: 200, BrotliSize: 50}, hydrate.RuntimeRef{Path: "/z.wasm", Size: 100}, true},
		{"each asset uses its best format", hydrate.RuntimeRef{Path: "/a.wasm", Size: 200, BrotliSize: 50}, hydrate.RuntimeRef{Path: "/z.wasm", Size: 100, GzipSize: 80}, true},
		{"Brotli tie uses raw", hydrate.RuntimeRef{Path: "/z.wasm", Size: 90, BrotliSize: 50}, hydrate.RuntimeRef{Path: "/a.wasm", Size: 100, BrotliSize: 50}, true},
		{"raw tie uses path", hydrate.RuntimeRef{Path: "/a.wasm", Size: 100}, hydrate.RuntimeRef{Path: "/z.wasm", Size: 100}, true},
		{"unknown raw sizes use path", hydrate.RuntimeRef{Path: "/a.wasm"}, hydrate.RuntimeRef{Path: "/z.wasm"}, true},
		{"known raw beats unknown", hydrate.RuntimeRef{Path: "/z.wasm", Size: 100}, hydrate.RuntimeRef{Path: "/a.wasm"}, true},
		{"unknown raw does not beat known", hydrate.RuntimeRef{Path: "/a.wasm"}, hydrate.RuntimeRef{Path: "/z.wasm", Size: 100}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runtimeRefIsSmaller(tc.candidate, tc.current); got != tc.want {
				t.Fatalf("runtimeRefIsSmaller(%+v, %+v) = %v, want %v", tc.candidate, tc.current, got, tc.want)
			}
		})
	}
}

func TestRendererSelectsCompressedRuntimeCostWithoutChangingCapabilities(t *testing.T) {
	r := NewRenderer("main")
	m := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{WASMVariants: map[string]buildmanifest.RuntimeVariantAsset{
		"core":   {HashedAsset: buildmanifest.HashedAsset{File: "core.wasm", Size: 666700, GzipSize: 280000, BrotliSize: 219927}, Variant: "core", FeatureMask: 17},
		"engine": {HashedAsset: buildmanifest.HashedAsset{File: "engine.wasm", Size: 666700, GzipSize: 280000, BrotliSize: 219517}, Variant: "engine", FeatureMask: 27},
	}}}
	if err := r.ApplyBuildManifest(m, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	r.RenderIsland("Counter", nil, gosx.Text("counter"))
	if got := r.selectedRuntimeRef(); got.Variant != "engine" || got.BrotliSize != 219517 || got.GzipSize != 280000 {
		t.Fatalf("selected compressed runtime = %+v", got)
	}
	page, err := r.ManifestJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, "brotliSize") || strings.Contains(page, "gzipSize") {
		t.Fatalf("server selection metadata must not add page bytes: %s", page)
	}

	core := m.Runtime.WASMVariants["core"]
	core.BrotliSize = 1
	m.Runtime.WASMVariants["core"] = core
	r.RenderEngine(engine.Config{Name: "Board", Kind: engine.KindSurface, Runtime: engine.RuntimeShared}, gosx.Node{})
	if err := r.ApplyBuildManifest(m, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	if got := r.selectedRuntimeRef().Variant; got != "engine" {
		t.Fatalf("cheaper incompatible core must not replace the engine: %s", got)
	}
}

func TestRendererCompressedCostFallsBackForOlderManifests(t *testing.T) {
	r := NewRenderer("main")
	m := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{WASMVariants: map[string]buildmanifest.RuntimeVariantAsset{
		"core":   {HashedAsset: buildmanifest.HashedAsset{File: "core.wasm", Size: 100}, Variant: "core", FeatureMask: 17},
		"engine": {HashedAsset: buildmanifest.HashedAsset{File: "engine.wasm", Size: 100}, Variant: "engine", FeatureMask: 27},
	}}}
	if err := r.ApplyBuildManifest(m, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	r.RenderIsland("Counter", nil, gosx.Text("counter"))
	if got := r.selectedRuntimeRef().Variant; got != "core" {
		t.Fatalf("equal raw sizes must use the path: %s", got)
	}
	core := m.Runtime.WASMVariants["core"]
	core.Size = 101
	m.Runtime.WASMVariants["core"] = core
	if err := r.ApplyBuildManifest(m, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	if got := r.selectedRuntimeRef().Variant; got != "engine" {
		t.Fatalf("older manifests must use raw size before path: %s", got)
	}
	for name, ref := range m.Runtime.WASMVariants {
		ref.Size = 0
		m.Runtime.WASMVariants[name] = ref
	}
	for i := 0; i < 100; i++ {
		if err := r.ApplyBuildManifest(m, "/gosx/assets"); err != nil {
			t.Fatal(err)
		}
		if got := r.selectedRuntimeRef().Variant; got != "core" {
			t.Fatalf("unknown sizes must use the path regardless of map order: %s", got)
		}
	}
}

func TestRendererLegacyRuntimeAssetsCarryCompressedCost(t *testing.T) {
	r := NewRenderer("main")
	m := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		WASM:        buildmanifest.HashedAsset{File: "full.wasm", Size: 1000, GzipSize: 30, BrotliSize: 10},
		WASMIslands: buildmanifest.HashedAsset{File: "islands.wasm", Size: 100, GzipSize: 40, BrotliSize: 20},
	}}
	if err := r.ApplyBuildManifest(m, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	r.RenderIsland("Counter", nil, gosx.Text("counter"))
	if got := r.selectedRuntimeRef(); got.Variant != "full" || got.BrotliSize != 10 || got.GzipSize != 30 {
		t.Fatalf("legacy compressed runtime selection = %+v", got)
	}
	if got := r.islandRuntime; got.BrotliSize != 20 || got.GzipSize != 40 {
		t.Fatalf("legacy islands cost was dropped: %+v", got)
	}
}

func TestRuntimeRefMixedMetadataHasConsistentOrdering(t *testing.T) {
	a := hydrate.RuntimeRef{Path: "/a.wasm", Size: 10, BrotliSize: 9}
	b := hydrate.RuntimeRef{Path: "/b.wasm", Size: 20, BrotliSize: 1}
	c := hydrate.RuntimeRef{Path: "/c.wasm", Size: 15}
	if !runtimeRefIsSmaller(b, a) || !runtimeRefIsSmaller(a, c) || !runtimeRefIsSmaller(b, c) {
		t.Fatal("mixed old and compressed metadata must preserve cost ordering")
	}
}
