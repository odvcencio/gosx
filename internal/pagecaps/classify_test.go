package pagecaps

import (
	"encoding/json"
	"reflect"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island"
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
		{Capabilities{Engines: 1, Scene3D: true, Runtime: "js"}, false, []string{"engine/js", "scene3d/js"}},
		{Capabilities{Engines: 1, Scene3D: true, WASM: true, Runtime: "shared"}, false, []string{"engine/shared", "scene3d/shared"}},
		{Capabilities{Engines: 1, Video: true, Runtime: "js"}, false, []string{"engine/js", "video"}},
		{Capabilities{Bootstrap: true, BootstrapMode: "preview"}, false, []string{"preview"}},
		{Capabilities{Hubs: 1}, false, []string{"enhanced"}},
		{Capabilities{Controllers: 1}, false, []string{"enhanced"}},
		{Capabilities{Engines: 1, Runtime: "js"}, true, []string{"engine/js", "game/js"}},
		{Capabilities{Islands: 1, Engines: 1, Scene3D: true, WASM: true, Runtime: "shared"}, true, []string{"engine/shared", "game/shared", "island", "scene3d/shared"}},
		{Capabilities{Islands: 1, Engines: 1, Scene3D: true, WASM: true, Runtime: "mixed"}, false, []string{"engine/js", "engine/shared", "go-wasm", "island", "scene3d/js", "scene3d/shared"}},
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

func TestClassifyRenderedVideoGame(t *testing.T) {
	for _, tc := range []struct {
		name    string
		runtime engine.Runtime
		want    []string
	}{
		{"javascript", "", []string{"game/js", "video"}},
		{"shared", engine.RuntimeShared, []string{"game/shared", "video"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			island.ResetPreviewBootstrap()
			t.Cleanup(island.ResetPreviewBootstrap)
			r := island.NewRenderer("main")
			r.RenderEngine(engine.Config{Name: "Video", Kind: engine.KindVideo, Runtime: tc.runtime}, gosx.Text(""))
			c, err := FromHTML([]byte(gosx.RenderHTML(r.PageHead())))
			if err != nil {
				t.Fatal(err)
			}
			if c.Engines != 1 || !c.Video || tc.runtime == engine.RuntimeShared && (!c.WASM || c.Runtime != "shared") {
				t.Fatalf("renderer video evidence missing: %+v", c)
			}
			got, err := Classify(c, true)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("classified %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClassifyJSONRoundTripPreservesGenericEngine(t *testing.T) {
	for _, tc := range []struct {
		name    string
		runtime engine.Runtime
		want    []string
	}{
		{"javascript", "", []string{"engine/js", "scene3d/js", "video"}},
		{"shared", engine.RuntimeShared, []string{"engine/shared", "scene3d/shared", "video"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			island.ResetPreviewBootstrap()
			t.Cleanup(island.ResetPreviewBootstrap)
			r := island.NewRenderer("main")
			r.RenderEngine(engine.Config{Name: "Video", Kind: engine.KindVideo, Runtime: tc.runtime}, gosx.Text(""))
			r.RenderEngine(engine.Config{Name: "Generic", Kind: engine.KindWorker, Runtime: tc.runtime}, gosx.Text(""))
			// This scene marker is independent of the two registered engines.
			html := `<div data-gosx-scene3d></div>` + gosx.RenderHTML(r.PageHead())
			c, err := FromHTML([]byte(html))
			if err != nil {
				t.Fatal(err)
			}
			before, err := Classify(c, false)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, tc.want) {
				t.Fatalf("rendered obligations %v, want %v", before, tc.want)
			}
			data, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			var restored Capabilities
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatal(err)
			}
			after, err := Classify(restored, false)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("round trip changed obligations: %v to %v", before, after)
			}
		})
	}
}
