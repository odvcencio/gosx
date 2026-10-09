package island

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/engine"
)

func TestRequiredFeatureAdvertisesPreloadNotScript(t *testing.T) {
	m := &buildmanifest.Manifest{}
	m.Runtime.BootstrapRuntime = buildmanifest.HashedAsset{File: "bootstrap-runtime.rt.js", Hash: "rt"}
	m.Runtime.BootstrapFeatureEngines = buildmanifest.HashedAsset{File: "bootstrap-feature-engines.en.js", Hash: "en"}
	m.Runtime.BootstrapFeatureTextlayout = buildmanifest.HashedAsset{File: "bootstrap-feature-textlayout.tl.js", Hash: "tl"}
	m.Runtime.Features = map[string]buildmanifest.HashedAsset{"engine-bridge": {File: "bootstrap-feature-engine-bridge.br.js", Hash: "br"}}
	r := NewRenderer("page")
	if err := r.ApplyBuildManifest(m, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	r.RenderEngine(engine.Config{Name: "Fixture", Kind: engine.KindWorker, Runtime: engine.RuntimeGoWASM, WASMPath: "/engines/f.wasm"}, gosx.Node{})
	if err := r.RequireFeature("engine-bridge"); err != nil {
		t.Fatal(err)
	}
	s := r.Summary()
	paths := r.FeaturePaths()
	bridge := paths["engine-bridge"]
	if !strings.Contains(bridge, "bootstrap-feature-engine-bridge.br.js") {
		t.Fatalf("FeaturePaths[engine-bridge] = %q", bridge)
	}
	if paths["engines"] != s.BootstrapFeatureEnginesPath || paths["engines"] == "" {
		t.Fatalf("engines must appear under FeaturePaths: %+v", paths)
	}
	hints := gosx.RenderHTML(r.PreloadHints())
	if !strings.Contains(hints, `rel="preload" href="`+bridge+`" as="script"`) {
		t.Fatalf("preload hint missing: %s", hints)
	}
	head := gosx.RenderHTML(r.PageHead())
	if strings.Contains(head, `src="`+bridge) {
		t.Fatal("a feature chunk must never be emitted as a script tag")
	}
	if !strings.Contains(head, `"features":["engine-bridge"]`) {
		t.Fatalf("manifest JSON lacks features: %s", head)
	}
}

func TestRequiredFeatureWithoutBuildManifestUsesCompatURL(t *testing.T) {
	r := NewRenderer("page")
	r.RenderEngine(engine.Config{Name: "Fixture", Kind: engine.KindWorker, Runtime: engine.RuntimeGoWASM, WASMPath: "/engines/f.wasm"}, gosx.Node{})
	if err := r.RequireFeature("painter"); err != nil {
		t.Fatal(err)
	}
	if got := r.FeaturePaths()["painter"]; !strings.HasSuffix(got, "/gosx/bootstrap-feature-painter.js") && !strings.Contains(got, "/gosx/bootstrap-feature-painter.js?v=") {
		t.Fatalf("FeaturePaths[painter] = %q", got)
	}
	if err := r.RequireFeature("Bad Name"); err == nil {
		t.Fatal("an invalid feature name must be rejected")
	}
	if _, ok := r.FeaturePaths()["Bad Name"]; ok {
		t.Fatal("a rejected feature must not be advertised")
	}
}

func TestSummaryStaysComparable(t *testing.T) {
	a, b := NewRenderer("page").Summary(), NewRenderer("page").Summary()
	if a != b {
		t.Fatal("Summary must stay comparable with ==")
	}
}

func TestSummaryFeaturePathsOmitsUnusedLegacyChunks(t *testing.T) {
	r := NewRenderer("page")
	r.EnableBootstrap()
	if got := r.FeaturePaths(); len(got) != 0 {
		t.Fatalf("a page with no engines or features must have no FeaturePaths, got %+v", got)
	}
}

func TestClientManifestDropsScene3DFeature(t *testing.T) {
	r := NewRenderer("page")
	r.RenderEngine(engine.Config{Name: "Fixture", Kind: engine.KindSurface}, gosx.Node{})
	// A caller can bypass RequireFeature by assigning the exported field.
	r.Manifest().Features = []string{"scene3d", "painter"}
	got := r.clientManifest().Features
	if len(got) != 1 || got[0] != "painter" {
		t.Fatalf("client manifest features = %v, want only painter", got)
	}
	json, err := r.ManifestJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(json, "scene3d") {
		t.Fatalf("the page manifest must not name scene3d as a feature: %s", json)
	}
}

func TestExplicitLegacyRequirementsAreAdvertised(t *testing.T) {
	r := NewRenderer("page")
	r.RenderEngine(engine.Config{Name: "Plain", Kind: engine.KindSurface}, gosx.Node{})
	for _, name := range []string{"hubs", "controllers", "islands"} {
		if err := r.RequireFeature(name); err != nil {
			t.Fatal(err)
		}
	}
	s := r.Summary()
	for name, path := range map[string]string{"hubs": s.BootstrapFeatureHubsPath, "controllers": s.BootstrapFeatureControllersPath, "islands": s.BootstrapFeatureIslandsPath} {
		if !strings.Contains(path, "bootstrap-feature-"+name) {
			t.Errorf("Summary lacks the %s chunk URL: %q", name, path)
		}
		if got := r.FeaturePaths()[name]; got != path {
			t.Errorf("FeaturePaths[%s] = %q, want %q", name, got, path)
		}
	}
	hints := gosx.RenderHTML(r.PreloadHints())
	for _, name := range []string{"hubs", "controllers", "islands"} {
		if !strings.Contains(hints, "bootstrap-feature-"+name) {
			t.Errorf("preload hints lack %s: %s", name, hints)
		}
	}
	// No entries of that kind: the explicit name alone selects the chunk, and
	// a page without the requirement does not.
	plain := NewRenderer("page")
	plain.RenderEngine(engine.Config{Name: "Plain", Kind: engine.KindSurface}, gosx.Node{})
	if got := plain.Summary().BootstrapFeatureHubsPath; got != "" {
		t.Errorf("hubs chunk advertised without a requirement: %q", got)
	}
}
