package pagecaps

import (
	"reflect"
	"testing"
)

func TestClassifyExperiences(t *testing.T) {
	for _, test := range []struct {
		c    Capabilities
		game bool
		want []string
	}{
		{Capabilities{}, false, []string{"static"}},
		{Capabilities{Navigation: true}, false, []string{"enhanced"}},
		{Capabilities{Bootstrap: true, BootstrapMode: "lite"}, false, []string{"enhanced"}},
		{Capabilities{Islands: 1, WASM: true, Runtime: "shared"}, false, []string{"island"}},
		{Capabilities{ComputeIslands: 1, WASM: true, Runtime: "shared"}, false, []string{"island"}},
		{Capabilities{Engines: 1, Runtime: "js"}, false, []string{"engine/js"}},
		{Capabilities{Engines: 1, WASM: true, Runtime: "shared"}, false, []string{"engine/shared"}},
		{Capabilities{Engines: 1, WASM: true, Runtime: "go-wasm"}, false, []string{"go-wasm"}},
		{Capabilities{Engines: 1, Scene3D: true, Runtime: "js"}, false, []string{"scene3d/js"}},
		{Capabilities{Engines: 1, Scene3D: true, WASM: true, Runtime: "shared"}, false, []string{"scene3d/shared"}},
		{Capabilities{Engines: 1, Video: true, Runtime: "js"}, false, []string{"video"}},
		{Capabilities{Bootstrap: true, BootstrapMode: "preview"}, false, []string{"preview"}},
		{Capabilities{Hubs: 1}, false, []string{"enhanced"}},
		{Capabilities{Controllers: 1}, false, []string{"enhanced"}},
		{Capabilities{Engines: 1, Runtime: "js"}, true, []string{"engine/js", "game/js"}},
		{Capabilities{Islands: 1, Engines: 1, Scene3D: true, WASM: true, Runtime: "shared"}, true, []string{"game/shared", "island", "scene3d/shared"}},
		{Capabilities{Islands: 1, Engines: 1, Scene3D: true, WASM: true, Runtime: "mixed"}, false, []string{"go-wasm", "island", "scene3d/js", "scene3d/shared"}},
	} {
		got, err := Classify(test.c, test.game)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("classification differs: %v want %v", got, test.want)
		}
	}
}

func TestClassifyRejectsUnsupportedDeclarations(t *testing.T) {
	for _, c := range []Capabilities{{Runtime: "unknown"}, {BootstrapMode: "unknown"}, {Islands: -1}, {ComputeIslands: -1}, {Engines: -1}, {Hubs: 1000001}, {Controllers: -1}, {WASM: true}, {Runtime: "shared"}} {
		if _, err := Classify(c, false); err == nil {
			t.Fatal("unsupported capabilities accepted")
		}
	}
	if _, err := Classify(Capabilities{Islands: 1}, true); err == nil {
		t.Fatal("game without an engine accepted")
	}
}

func TestClassifyDeclarationPreservesDetectedObligations(t *testing.T) {
	c := Capabilities{Islands: 1, ComputeIslands: 1, Engines: 1, Scene3D: true, Video: true, WASM: true, Runtime: "shared", BootstrapMode: "preview"}
	detected, err := Classify(c, false)
	if err != nil {
		t.Fatal(err)
	}
	declared, err := Classify(c, true)
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	for _, name := range declared {
		set[name] = true
	}
	for _, name := range detected {
		if !set[name] {
			t.Fatal("declaration removed detected requirement", name)
		}
	}
	if !set["game/shared"] {
		t.Fatal("game declaration was ignored")
	}
}

func TestClassifyDecodedEngineEvidence(t *testing.T) {
	for _, test := range []struct {
		body string
		game bool
		want []string
	}{
		{`{"engines":[{"component":"GoSXScene3D"}]}`, false, []string{"scene3d/js"}},
		{`{"islands":[{}],"runtime":{"path":"core.wasm"},"engines":[{"component":"GoSXScene3D"}]}`, true, []string{"game/js", "island", "scene3d/js"}},
		{`{"engines":[{}, {"runtime":"shared"}]}`, true, []string{"engine/js", "engine/shared", "game/js", "game/shared"}},
		{`{"engines":[{"component":"GoSXScene3D"},{}]}`, false, []string{"engine/js", "scene3d/js"}},
		{`{"engines":[{"component":"GoSXScene3D","runtime":"shared"},{"runtime":"go-wasm","programRef":"app.wasm"}]}`, false, []string{"go-wasm", "scene3d/shared"}},
		{`{"engines":[{"kind":"video"},{}]}`, false, []string{"engine/js", "video"}},
	} {
		c, err := FromHTML([]byte(manifestHTML(test.body)))
		if err != nil {
			t.Fatal(err)
		}
		got, err := Classify(c, test.game)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("classified %v, want %v", got, test.want)
		}
	}
}
