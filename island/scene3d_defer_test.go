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
		Defer:                &scene.Defer{Public: true},
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
	if strings.Contains(hints, "bootstrap-feature-scene3d.js") || strings.Contains(hints, "/models/hero.glb") {
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
	// A gated scene runs through the scheduler with no wait, and the page
	// loads the chunk that holds the scheduler, the probes and the publisher.
	if !strings.Contains(manifest, `"defer":{"until":"now"`) || !strings.Contains(manifest, `"scene3d-defer"`) {
		t.Fatalf("gated scene must carry defer=now and require the scene3d-defer feature: %s", manifest)
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

func TestDeferVariantsAreStoredAndListedByMedia(t *testing.T) {
	store := useTestSceneEntryStore(t)
	r := NewRenderer("main")
	base := scene.Props{Graph: scene.NewGraph(scene.Model{ID: "m", Src: "/models/a.glb"})}
	small := base
	small.Width = 320
	base.Defer = &scene.Defer{Public: true, Variants: []scene.DeferVariant{{Media: "(max-width: 767px), (pointer: coarse)", Props: &small}}}
	cfg := base.EngineConfig()
	cfg.MountID = "hero"
	r.RenderEngine(cfg, gosx.Text(""))
	manifest, _ := r.ManifestJSON()
	if !strings.Contains(manifest, `"variants":[{"media":"(max-width: 767px), (pointer: coarse)","propsRef":"/gosx/scene3d/`) {
		t.Fatalf("variant missing: %s", manifest)
	}
	if strings.Contains(manifest, "deferVariants") {
		t.Fatalf("variant props must not stay inline: %s", manifest)
	}
	var m struct {
		Engines []struct {
			PropsRef string
			Defer    struct{ Variants []struct{ PropsRef string } }
		}
	}
	if err := json.Unmarshal([]byte(manifest), &m); err != nil {
		t.Fatal(err)
	}
	variantHash := strings.TrimSuffix(strings.TrimPrefix(m.Engines[0].Defer.Variants[0].PropsRef, SceneEntryPathPrefix), ".json")
	stored, ok := store.Get(variantHash)
	if !ok || !strings.Contains(string(stored), `"width":320`) || strings.Contains(string(stored), "deferVariants") {
		t.Fatalf("variant body = %s", stored)
	}
	baseHash := strings.TrimSuffix(strings.TrimPrefix(m.Engines[0].PropsRef, SceneEntryPathPrefix), ".json")
	if body, _ := store.Get(baseHash); strings.Contains(string(body), "deferVariants") {
		t.Fatalf("base body must not carry the variants: %s", body)
	}
}

func TestDeferVariantsAreDroppedWhenStoreDisabled(t *testing.T) {
	prev := DefaultSceneEntryStore()
	t.Cleanup(func() { SetSceneEntryStore(prev) })
	SetSceneEntryStore(nil)
	r := NewRenderer("main")
	small := scene.Props{Width: 320}
	base := scene.Props{Defer: &scene.Defer{Public: true, Variants: []scene.DeferVariant{{Media: "(max-width: 767px)", Props: &small}}}}
	cfg := base.EngineConfig()
	cfg.MountID = "hero"
	r.RenderEngine(cfg, gosx.Text(""))
	manifest, _ := r.ManifestJSON()
	if strings.Contains(manifest, "deferVariants") || strings.Contains(manifest, `"variants"`) {
		t.Fatalf("inline props must not carry variants: %s", manifest)
	}
}

func TestPlainSceneDoesNotRequireTheDeferChunk(t *testing.T) {
	r := NewRenderer("main")
	plain := scene.Props{Graph: scene.NewGraph(scene.Model{ID: "p", Src: "/models/p.glb"})}.EngineConfig()
	plain.MountID = "plain"
	r.RenderEngine(plain, gosx.Text(""))
	manifest, _ := r.ManifestJSON()
	if strings.Contains(manifest, "scene3d-defer") || strings.Contains(manifest, `"defer"`) {
		t.Fatalf("a scene with no defer and no gate must not pay for the defer chunk: %s", manifest)
	}
}

func TestDeferredPropsRefNamesItsRoute(t *testing.T) {
	useTestSceneEntryStore(t)
	r := NewRenderer("main")
	r.SetRoute("/galaxy")
	r.SetSceneEntrySecret([]byte("0123456789abcdef"))
	r.RenderEngine(deferredSceneConfig("hero"), gosx.Text(""))
	manifest, _ := r.ManifestJSON()
	if !strings.Contains(manifest, `.json?r=%2Fgalaxy\u0026s=`) {
		t.Fatalf("props URL must name its route: %s", manifest)
	}
	r = NewRenderer("main")
	r.SetRoute("/gosx/scene3d/x.json")
	r.SetSceneEntrySecret([]byte("0123456789abcdef"))
	r.RenderEngine(deferredSceneConfig("hero"), gosx.Text(""))
	manifest, _ = r.ManifestJSON()
	if strings.Contains(manifest, "?r=") {
		t.Fatalf("a framework route must not be named: %s", manifest)
	}
}

func TestStoredDeferredPropsCarryTheBasePath(t *testing.T) {
	store := useTestSceneEntryStore(t)
	r := NewRenderer("main")
	r.SetBasePath("/.proxy/game")
	small := scene.Props{Graph: scene.NewGraph(scene.Model{ID: "m", Src: "/models/small.glb"})}
	base := scene.Props{
		Graph: scene.NewGraph(scene.Model{ID: "m", Src: "/models/hero.glb"}),
		Defer: &scene.Defer{Public: true, Variants: []scene.DeferVariant{{Media: "(max-width: 767px)", Props: &small}}},
	}
	cfg := base.EngineConfig()
	cfg.MountID = "hero"
	r.RenderEngine(cfg, gosx.Text(""))
	manifest, _ := r.ManifestJSON()
	var m struct {
		Engines []struct {
			PropsRef string
			Defer    struct{ Variants []struct{ PropsRef string } }
		}
	}
	if err := json.Unmarshal([]byte(manifest), &m); err != nil {
		t.Fatal(err)
	}
	hashOf := func(ref string) string {
		ref = strings.TrimPrefix(ref, "/.proxy/game")
		return strings.TrimSuffix(strings.TrimPrefix(ref, SceneEntryPathPrefix), ".json")
	}
	for name, ref := range map[string]string{"base": m.Engines[0].PropsRef, "variant": m.Engines[0].Defer.Variants[0].PropsRef} {
		body, ok := store.Get(hashOf(ref))
		if !ok {
			t.Fatalf("%s props not stored under %s", name, ref)
		}
		if !strings.Contains(string(body), `"/.proxy/game/models/`) || strings.Contains(string(body), `"/models/`) {
			t.Fatalf("%s props must carry the base path: %s", name, body)
		}
	}
	if !strings.Contains(manifest, `"href":"/.proxy/game/models/hero.glb"`) {
		t.Fatalf("preload hint lost the base path: %s", manifest)
	}
}

func TestNonPublicDeferKeepsPropsInline(t *testing.T) {
	store := useTestSceneEntryStore(t)
	r := NewRenderer("main")
	r.SetRoute("/account")
	r.SetSceneEntrySecret([]byte("0123456789abcdef"))
	cfg := scene.Props{Label: "for alice", Defer: &scene.Defer{}, Graph: scene.NewGraph(scene.Model{ID: "m", Src: "/models/a.glb"})}.EngineConfig()
	cfg.MountID = "hero"
	r.RenderEngine(cfg, gosx.Text(""))
	manifest, _ := r.ManifestJSON()
	if strings.Contains(manifest, "propsRef") || !strings.Contains(manifest, "for alice") || !strings.Contains(manifest, `"defer":{"until":"idle"`) {
		t.Fatalf("non-public props must stay inline and still defer: %s", manifest)
	}
	if _, ok := store.Get(SceneEntryHash(cfg.Props)); ok {
		t.Fatal("non-public props must not enter the shared store")
	}
}

func TestSceneEntrySignatureBindsRouteAndHash(t *testing.T) {
	secret := []byte("0123456789abcdef")
	hash := SceneEntryHash([]byte("x"))
	sig := SignSceneEntry(secret, "/galaxy", hash)
	if !VerifySceneEntry(secret, "/galaxy", hash, sig) {
		t.Fatal("valid signature rejected")
	}
	for name, ok := range map[string]bool{
		"route":  VerifySceneEntry(secret, "/other", hash, sig),
		"hash":   VerifySceneEntry(secret, "/galaxy", SceneEntryHash([]byte("y")), sig),
		"secret": VerifySceneEntry([]byte("another-secret-key"), "/galaxy", hash, sig),
		"empty":  VerifySceneEntry(nil, "/galaxy", hash, sig),
		"short":  VerifySceneEntry(secret, "/galaxy", hash, "abc"),
	} {
		if ok {
			t.Fatalf("signature accepted for a changed %s", name)
		}
	}
	r := NewRenderer("main")
	r.SetRoute("/galaxy")
	r.SetSceneEntrySecret(secret)
	useTestSceneEntryStore(t)
	r.RenderEngine(deferredSceneConfig("hero"), gosx.Text(""))
	manifest, _ := r.ManifestJSON()
	if !strings.Contains(manifest, `?r=%2Fgalaxy\u0026s=`) {
		t.Fatalf("props URL must carry the signed route: %s", manifest)
	}
}

func TestSceneEntryWatchCapturesWhatTheStoreEvicts(t *testing.T) {
	s := NewSceneEntryStore(2, 1<<20)
	target := SceneEntryHash([]byte("target"))
	w := s.Watch(target)
	defer w.Close()
	s.Put([]byte("target"))
	for i := 0; i < 5; i++ {
		s.Put([]byte{byte('a' + i)})
	}
	if _, ok := s.Get(target); ok {
		t.Fatal("test setup: the entry should be evicted")
	}
	if body, ok := w.Body(); !ok || string(body) != "target" {
		t.Fatalf("watch lost the bytes: %q %v", body, ok)
	}
}

func TestSceneEntryStoreBoundsComeFromTheEnvironment(t *testing.T) {
	env := func(name string) string {
		return map[string]string{"GOSX_SCENE_ENTRY_MAX_ENTRIES": "3", "GOSX_SCENE_ENTRY_MAX_BYTES": "100"}[name]
	}
	s := sceneEntryStoreFromEnv(env)
	if s.maxEntries != 3 || s.maxBytes != 100 {
		t.Fatalf("bounds = %d, %d", s.maxEntries, s.maxBytes)
	}
	if d := sceneEntryStoreFromEnv(func(string) string { return "" }); d.maxEntries < 256 {
		t.Fatalf("default bound %d is too small for a page with many scenes", d.maxEntries)
	}
}

func TestSceneEntryWatchersForTheSameHashAllReceiveIt(t *testing.T) {
	s := NewSceneEntryStore(8, 1<<20)
	hash := SceneEntryHash([]byte("shared"))
	a, b := s.Watch(hash), s.Watch(hash)
	a.Close() // the first watcher leaves; the second must keep its place
	s.Put([]byte("shared"))
	if _, ok := a.Body(); ok {
		t.Fatal("a closed watcher must not capture")
	}
	if body, ok := b.Body(); !ok || string(body) != "shared" {
		t.Fatalf("second watcher = %q %v", body, ok)
	}
	b.Close()
	if len(s.watches) != 0 {
		t.Fatalf("watch lists leaked: %d", len(s.watches))
	}
}
