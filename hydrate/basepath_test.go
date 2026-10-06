package hydrate

import (
	"encoding/json"
	"m31labs.dev/gosx/controller"
	"testing"
)

func TestManifestBasePathCopiesURLsAndKeepsProps(t *testing.T) {
	m := NewManifest()
	m.Runtime = RuntimeRef{Path: "/gosx/runtime.wasm"}
	m.Bundles["main"] = BundleRef{Path: "/gosx/runtime.wasm"}
	m.Islands = []IslandEntry{{ProgramRef: "/gosx/islands/counter.gxi", Props: json.RawMessage(`{"text":"/unchanged"}`)}}
	m.ComputeIslands = []ComputeIslandEntry{{ProgramRef: "/gosx/compute.gxi"}}
	m.Engines = []EngineEntry{{ProgramRef: "/gosx/engine.wasm"}, {ProgramRef: "https://assets.example/engine.wasm"}}
	m.Hubs = []HubEntry{{Path: "/ws", Input: &HubInputConfig{CPUEndpoint: "/api/start"}}, {Path: "wss://realtime.example/ws"}}
	m.Controllers = []ControllerEntry{{Config: controller.Config{Resources: []controller.FetchResource{{URL: "/api/state"}}}}}
	m.TextureVariants = map[string][]ManifestVariantRef{"/texture.png": {{URI: "/texture.ktx2"}}}
	before, _ := m.Marshal()
	out := m.WithBasePath("/.proxy/game")
	if out.Runtime.Path != "/.proxy/game/gosx/runtime.wasm" || out.Islands[0].ProgramRef != "/.proxy/game/gosx/islands/counter.gxi" || out.ComputeIslands[0].ProgramRef != "/.proxy/game/gosx/compute.gxi" || out.Engines[0].ProgramRef != "/.proxy/game/gosx/engine.wasm" || out.Hubs[0].Path != "/.proxy/game/ws" || out.Controllers[0].Config.Resources[0].URL != "/.proxy/game/api/state" || out.Hubs[0].Input.CPUEndpoint != "/.proxy/game/api/start" || out.TextureVariants["/texture.png"][0].URI != "/.proxy/game/texture.ktx2" {
		t.Fatalf("URLs: %+v", out)
	}
	if out.Engines[1].ProgramRef != m.Engines[1].ProgramRef || out.Hubs[1].Path != m.Hubs[1].Path || string(out.Islands[0].Props) != string(m.Islands[0].Props) {
		t.Fatal("external URLs or props changed")
	}
	after, _ := m.Marshal()
	if string(before) != string(after) {
		t.Fatal("shared manifest mutated")
	}
	twice, _ := out.WithBasePath("/.proxy/game").Marshal()
	once, _ := out.Marshal()
	if string(twice) != string(once) {
		t.Fatal("double prefix")
	}
}

func TestTextureVariantsMatchPublicModelRelativePaths(t *testing.T) {
	m := NewManifest()
	m.TextureVariants = map[string][]ManifestVariantRef{
		"assets/wood.png":               {{URI: "/assets/wood.ktx2"}},
		"/assets/stone.png":             {{URI: "/assets/stone.ktx2"}},
		"https://cdn.example/other.png": {{URI: "https://cdn.example/other.ktx2"}},
	}
	out := m.WithBasePath("/.proxy/game")
	for _, source := range []string{"wood", "stone"} {
		// ../assets/<source>.png resolved against /.proxy/game/models/city.gltf.
		key := "/.proxy/game/assets/" + source + ".png"
		if refs := out.TextureVariants[key]; len(refs) != 1 || refs[0].URI != "/.proxy/game/assets/"+source+".ktx2" {
			t.Fatalf("public texture lookup %s: %+v", key, refs)
		}
	}
	if len(m.TextureVariants) != 3 || m.TextureVariants["assets/wood.png"][0].URI != "/assets/wood.ktx2" {
		t.Fatal("authored manifest mutated")
	}
	if len(out.TextureVariants) != 5 {
		t.Fatal("external key rewritten", out.TextureVariants)
	}
	once, _ := out.Marshal()
	twice, _ := out.WithBasePath("/.proxy/game").Marshal()
	if string(once) != string(twice) {
		t.Fatal("public aliases are not idempotent")
	}
}
