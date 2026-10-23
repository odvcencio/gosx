package hydrate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEngineEntryDeferMarshalsAndTakesBasePath(t *testing.T) {
	m := NewManifest()
	if _, err := m.AddEngineWithRuntime("GoSXScene3D", "surface", "", "hero", "", map[string]any{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	m.Engines[0].Props = json.RawMessage("null")
	m.Engines[0].PropsRef = "/gosx/scene3d/abc.json"
	m.Engines[0].Defer = &EngineDefer{
		Until:    "idle",
		Preloads: []EnginePreload{{Href: "/models/a.glb", As: "fetch"}, {Href: "https://cdn.example/x.ktx2", As: "fetch"}},
		Variants: []EngineDeferVariant{{Media: "(max-width: 767px)", PropsRef: "/gosx/scene3d/def.json"}},
	}
	out := m.WithBasePath("/app")
	e := out.Engines[0]
	if e.PropsRef != "/app/gosx/scene3d/abc.json" || e.Defer.Preloads[0].Href != "/app/models/a.glb" || e.Defer.Variants[0].PropsRef != "/app/gosx/scene3d/def.json" {
		t.Fatalf("base path not applied: %+v", e)
	}
	if e.Defer.Preloads[1].Href != "https://cdn.example/x.ktx2" {
		t.Fatalf("absolute hint must stay: %s", e.Defer.Preloads[1].Href)
	}
	if m.Engines[0].PropsRef != "/gosx/scene3d/abc.json" {
		t.Fatal("WithBasePath must not mutate the source manifest")
	}
	data, err := out.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"propsRef":"/app/gosx/scene3d/abc.json"`, `"defer":{"until":"idle"`, `"props":null`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("manifest JSON lacks %s: %s", want, data)
		}
	}
	plain := NewManifest()
	if _, err := plain.AddEngine("Other", "worker", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if data, _ := plain.Marshal(); strings.Contains(string(data), "propsRef") || strings.Contains(string(data), `"defer"`) {
		t.Fatalf("an eager entry must not carry defer keys: %s", data)
	}
}
