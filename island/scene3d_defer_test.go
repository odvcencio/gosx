package island

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
)

func deferredSceneConfig(mountID string, gates ...string) engine.Config {
	p := scene.Props{
		Defer:                &scene.Defer{},
		RequiredCapabilities: gates,
		Graph:                scene.NewGraph(scene.Model{ID: "m-" + mountID, Src: "/models/" + mountID + ".glb"}),
	}
	cfg := p.EngineConfig()
	cfg.MountID = mountID
	return cfg
}

func useTestSceneEntryStore(t *testing.T) *SceneEntryStore {
	t.Helper()
	prev := DefaultSceneEntryStore()
	s := NewSceneEntryStore(8, 1<<22)
	SetSceneEntryStore(s)
	t.Cleanup(func() { SetSceneEntryStore(prev) })
	return s
}

func TestDeferredSceneMovesPropsToContentAddressedURL(t *testing.T) {
	store := useTestSceneEntryStore(t)
	r := NewRenderer("main")
	cfg := deferredSceneConfig("hero")
	mount := gosx.RenderHTML(r.RenderEngine(cfg, gosx.El("img", gosx.Attrs(gosx.Attr("src", "/poster.webp")))))
	if !strings.Contains(mount, `data-gosx-scene3d-defer="idle"`) || !strings.Contains(mount, `src="/poster.webp"`) {
		t.Fatalf("mount = %s", mount)
	}
	manifest, err := r.ManifestJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"propsRef":"/gosx/scene3d/`, `"props":null`, `"defer":{"until":"idle"`, `{"href":"/models/hero.glb","as":"fetch"}`} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("manifest lacks %s: %s", want, manifest)
		}
	}
	stored, ok := store.Get(SceneEntryHash(cfg.Props))
	var a, b any
	if !ok || json.Unmarshal(stored, &a) != nil || json.Unmarshal(cfg.Props, &b) != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("store must hold the props bytes under their hash")
	}
	head := gosx.RenderHTML(r.BootstrapScript())
	if strings.Contains(head, `data-gosx-script="feature-scene3d"`) {
		t.Fatalf("a deferred-only page must not load the scene chunk eagerly: %s", head)
	}
	if !strings.Contains(head, `data-gosx-scene3d-url="`) || !strings.Contains(head, `data-gosx-scene3d-webgl-url="`) {
		t.Fatalf("the bootstrap tag must advertise the scene chunk and sub-chunk URLs: %s", head)
	}
	hints := gosx.RenderHTML(r.PreloadHints())
	if strings.Contains(hints, "bootstrap-feature-scene3d") || strings.Contains(hints, "/models/hero.glb") {
		t.Fatalf("a deferred scene must not preload at parse time: %s", hints)
	}
}

func TestTwoDeferredScenesGetDistinctRefs(t *testing.T) {
	useTestSceneEntryStore(t)
	r := NewRenderer("main")
	r.RenderEngine(deferredSceneConfig("a"), gosx.Text(""))
	r.RenderEngine(deferredSceneConfig("b"), gosx.Text(""))
	manifest, _ := r.ManifestJSON()
	var m struct {
		Engines []struct{ ID, PropsRef string }
	}
	if err := json.Unmarshal([]byte(manifest), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Engines) != 2 || m.Engines[0].PropsRef == "" || m.Engines[0].PropsRef == m.Engines[1].PropsRef || m.Engines[0].ID == m.Engines[1].ID {
		t.Fatalf("engines = %+v", m.Engines)
	}
}

func TestEagerAndDeferredScenesShareOnePage(t *testing.T) {
	useTestSceneEntryStore(t)
	r := NewRenderer("main")
	eager := scene.Props{Graph: scene.NewGraph(scene.Model{ID: "e", Src: "/models/eager.glb"})}.EngineConfig()
	eager.MountID = "eager"
	r.RenderEngine(eager, gosx.Text(""))
	r.RenderEngine(deferredSceneConfig("late"), gosx.Text(""))
	head := gosx.RenderHTML(r.BootstrapScript())
	hints := gosx.RenderHTML(r.PreloadHints())
	if !strings.Contains(head, `data-gosx-script="feature-scene3d"`) {
		t.Fatalf("an eager scene keeps the eager chunk tag: %s", head)
	}
	if !strings.Contains(hints, "/models/eager.glb") || strings.Contains(hints, "/models/late.glb") {
		t.Fatalf("only the eager scene preloads in the head: %s", hints)
	}
}

func TestEnvironmentGateDefersChunkLoad(t *testing.T) {
	r := NewRenderer("main")
	gated := scene.Props{RequiredCapabilities: []string{"hardware-gpu"}}.EngineConfig()
	gated.MountID = "gated"
	r.RenderEngine(gated, gosx.Text(""))
	head := gosx.RenderHTML(r.BootstrapScript())
	if strings.Contains(head, `data-gosx-script="feature-scene3d"`) || !strings.Contains(head, `data-gosx-scene3d-url="`) {
		t.Fatalf("a gated scene must load its chunk after the gate: %s", head)
	}
	manifest, _ := r.ManifestJSON()
	if !strings.Contains(manifest, `"requiredCapabilities":["hardware-gpu"]`) {
		t.Fatalf("gate missing from manifest: %s", manifest)
	}
}

func TestDeferInlineKeepsPropsWhenStoreDisabled(t *testing.T) {
	prev := DefaultSceneEntryStore()
	t.Cleanup(func() { SetSceneEntryStore(prev) })
	SetSceneEntryStore(nil)
	r := NewRenderer("main")
	r.RenderEngine(deferredSceneConfig("inline"), gosx.Text(""))
	manifest, _ := r.ManifestJSON()
	if strings.Contains(manifest, "propsRef") || !strings.Contains(manifest, `"defer":{"until":"idle"`) || strings.Contains(manifest, `"props":null`) {
		t.Fatalf("disabled store must keep props inline: %s", manifest)
	}
}
