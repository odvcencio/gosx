package pagecaps

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island"
)

func manifestHTML(body string) string {
	return "<script id=\"gosx-manifest\" type=\"application/json\">" + body + "</script>"
}

func TestPageCapsRenderedShapes(t *testing.T) {
	for _, test := range []struct {
		name, html string
		want       Capabilities
	}{
		{"static", "<main>Content</main>", Capabilities{BootstrapMode: "none", Runtime: "none"}},
		{"navigation", "<script defer data-gosx-navigation src=\"navigation.hash.js\"></script>", Capabilities{Navigation: true, BootstrapMode: "none", Runtime: "js"}},
		{"lite", "<script data-gosx-script=bootstrap data-gosx-bootstrap-mode=lite></script>", Capabilities{Bootstrap: true, BootstrapMode: "lite", Runtime: "js"}},
		{"island", manifestHTML("{\"islands\":[{}],\"runtime\":{\"path\":\"core.wasm\"}}"), Capabilities{Bootstrap: true, WASM: true, Islands: 1, BootstrapMode: "none", Runtime: "shared"}},
		{"compute-only", manifestHTML("{\"computeIslands\":[{}],\"runtime\":{\"path\":\"core.wasm\"}}"), Capabilities{Bootstrap: true, WASM: true, ComputeIslands: 1, BootstrapMode: "none", Runtime: "shared"}},
		{"dormant", manifestHTML("{\"bundles\":{\"page\":{\"path\":\"full.wasm\"}},\"runtime\":{\"path\":\"\"}}"), Capabilities{BootstrapMode: "none", Runtime: "none"}},
		{"loader-only", "<script defer data-gosx-script=wasm-exec src=\"exec.js\"></script>", Capabilities{BootstrapMode: "none", Runtime: "js"}},
		{"js-engine", manifestHTML("{\"engines\":[{\"runtime\":\"\"}]}"), Capabilities{Bootstrap: true, Engines: 1, BootstrapMode: "none", Runtime: "js"}},
		{"shared-engine", manifestHTML("{\"engines\":[{\"runtime\":\"shared\"}]}"), Capabilities{Bootstrap: true, Engines: 1, WASM: true, BootstrapMode: "none", Runtime: "shared"}},
		{"go-wasm", manifestHTML("{\"engines\":[{\"runtime\":\"go-wasm\",\"programRef\":\"app.wasm\"}]}"), Capabilities{Bootstrap: true, Engines: 1, WASM: true, BootstrapMode: "none", Runtime: "go-wasm"}},
		{"scene-ir", manifestHTML("{\"engines\":[{\"component\":\"GoSXScene3D\",\"props\":{\"SceneIR\":{}}}]}"), Capabilities{Bootstrap: true, Scene3D: true, Engines: 1, BootstrapMode: "none", Runtime: "js"}},
		{"shared-scene", manifestHTML("{\"engines\":[{\"component\":\"GoSXScene3D\",\"runtime\":\"shared\"}]}"), Capabilities{Bootstrap: true, Scene3D: true, Engines: 1, WASM: true, BootstrapMode: "none", Runtime: "shared"}},
		{"video", manifestHTML("{\"engines\":[{\"kind\":\"video\"}]}"), Capabilities{Bootstrap: true, Video: true, Engines: 1, BootstrapMode: "none", Runtime: "js"}},
		{"controller", manifestHTML("{\"controllers\":[{}]}"), Capabilities{Bootstrap: true, Controllers: 1, BootstrapMode: "none", Runtime: "js"}},
		{"hub", manifestHTML("{\"hubs\":[{}]}"), Capabilities{Bootstrap: true, Hubs: 1, BootstrapMode: "none", Runtime: "js"}},
		{"motion", "<div data-gosx-enhance=motion></div>", Capabilities{Motion: true, BootstrapMode: "none", Runtime: "none"}},
		{"preview", "<script data-gosx-script=bootstrap data-gosx-bootstrap-mode=preview></script>", Capabilities{Bootstrap: true, WASM: true, BootstrapMode: "preview", Runtime: "shared"}},
		{"mixed", manifestHTML("{\"computeIslands\":[{}],\"runtime\":{\"path\":\"core.wasm\"},\"engines\":[{\"runtime\":\"go-wasm\",\"programRef\":\"app.wasm\"}]}"), Capabilities{Bootstrap: true, WASM: true, Engines: 1, ComputeIslands: 1, BootstrapMode: "none", Runtime: "mixed"}},
		{"inert-template", "<template><script data-gosx-script=bootstrap></script></template>", Capabilities{BootstrapMode: "none", Runtime: "none"}},
		{"event-handler", "<button onclick=run()>Run</button>", Capabilities{BootstrapMode: "none", Runtime: "js"}},
		{"script-link", "<a href=javascript:run()>Run</a>", Capabilities{BootstrapMode: "none", Runtime: "js"}},
		{"inert-json", "<script type=application/json>{}</script>", Capabilities{BootstrapMode: "none", Runtime: "none"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := FromHTML([]byte(test.html))
			if err != nil {
				t.Fatal(err)
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(test.want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("capabilities differ: got %s want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestPageCapsRejectMalformedContracts(t *testing.T) {
	for _, body := range []string{"{", "null", "[]", "{\"engines\":[{\"runtime\":\"unknown\"}]}", "{\"engines\":[{\"runtime\":\"go-wasm\"}]}"} {
		if _, err := FromHTML([]byte(manifestHTML(body))); err == nil {
			t.Fatal("malformed manifest accepted")
		}
	}
	if _, err := FromHTML([]byte(manifestHTML("{}") + manifestHTML("{}"))); err == nil {
		t.Fatal("duplicate manifest accepted")
	}
	if _, err := FromHTML([]byte{0xff}); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	if _, err := FromHTML([]byte(strings.Repeat(" ", (16<<20)+1))); err == nil {
		t.Fatal("oversized HTML accepted")
	}
}

func TestPageCapsJSONVocabulary(t *testing.T) {
	data, err := json.Marshal(Capabilities{})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"navigation", "bootstrap", "wasm", "scene3d", "video", "motion", "bootstrapMode", "islands", "computeIslands", "engines", "hubs", "controllers", "runtime"} {
		if _, ok := fields[name]; !ok {
			t.Fatal("missing serialized capability", name)
		}
	}
	if len(fields) != 13 {
		t.Fatal("unexpected serialized capability")
	}
}

func TestPageCapsRejectConflictingBootstrap(t *testing.T) {
	for _, input := range []string{
		`<script data-gosx-script=bootstrap data-gosx-bootstrap-mode=unknown></script>`,
		`<script data-gosx-script=bootstrap data-gosx-bootstrap-mode=preview></script><script data-gosx-script=bootstrap data-gosx-bootstrap-mode=lite></script>`,
	} {
		if _, err := FromHTML([]byte(input)); err == nil {
			t.Fatal("invalid bootstrap contract accepted")
		}
	}
}

func TestPageCapsActualRendererContracts(t *testing.T) {
	for _, test := range []struct {
		name                 string
		prepare              func(*island.Renderer)
		mode, runtime, class string
	}{
		{"static", func(*island.Renderer) {}, "none", "none", "static"},
		{"lite", func(r *island.Renderer) { r.EnableBootstrap() }, "lite", "js", "enhanced"},
		{"preview", func(*island.Renderer) { island.EnablePreviewBootstrap() }, "preview", "shared", "preview"},
		{"compute", func(r *island.Renderer) {
			if _, err := r.RegisterComputeIsland(island.ComputeIslandConfig{Name: "Counter"}); err != nil {
				t.Fatal(err)
			}
		}, "full", "shared", "island"},
		{"scene-js", func(r *island.Renderer) {
			r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface}, gosx.Text(""))
		}, "full", "js", "scene3d/js"},
		{"go-wasm", func(r *island.Renderer) {
			r.RenderEngine(engine.Config{Name: "Example", Kind: engine.KindWorker, Runtime: engine.Runtime("go-wasm"), WASMPath: "/app/example.wasm"}, gosx.Text(""))
		}, "full", "go-wasm", "go-wasm"},
	} {
		t.Run(test.name, func(t *testing.T) {
			island.ResetPreviewBootstrap()
			t.Cleanup(island.ResetPreviewBootstrap)
			r := island.NewRenderer("main")
			test.prepare(r)
			c, err := FromHTML([]byte(gosx.RenderHTML(r.PageHead())))
			if err != nil {
				t.Fatal(err)
			}
			if c.BootstrapMode != test.mode || c.Runtime != test.runtime {
				t.Fatalf("mode/runtime %s/%s want %s/%s", c.BootstrapMode, c.Runtime, test.mode, test.runtime)
			}
			classes, err := Classify(c, false)
			if err != nil || len(classes) != 1 || classes[0] != test.class {
				t.Fatalf("classes %v: %v", classes, err)
			}
		})
	}
}
