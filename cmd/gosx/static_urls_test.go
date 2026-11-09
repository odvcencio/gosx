package main

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

const staticFixture = `<!DOCTYPE html><html><head><title>t</title></head><body>
<div id="scene"></div>
<script defer data-gosx-script="feature-scene3d" data-gosx-scene3d-gltf-url="/gosx/assets/runtime/bootstrap-feature-scene3d-gltf.aa.js" data-gosx-scene3d-gltf-integrity="sha256-x" src="/gosx/assets/runtime/bootstrap-feature-scene3d.bb.js"></script>
<script id="gosx-manifest" type="application/json">{"version":"0.1.0","islands":[{"programRef":"/gosx/assets/islands/Game.11.gxi"}],"engines":[{"props":{"model":"/models/ship.glb","label":"/not-a-file","scene":{"texture":"/tex/a.png?v=2"}}}],"bundles":{"gosx-page":{"path":"/gosx/assets/runtime/gosx-runtime.dd.wasm"}},"runtime":{"path":"/gosx/assets/runtime/gosx-runtime-core.cc.wasm","size":12}}</script>
<script id="gosx-document" type="application/json" data-gosx-document-contract="">{"page":{"path":"/play","pattern":"/play"},"assets":{"runtimePath":"/gosx/assets/runtime/gosx-runtime-core.cc.wasm","wasmExecPath":"/gosx/assets/runtime/wasm_exec.ee.js"}}</script>
</body></html>`

func fixtureOpts(prefix string) staticRewriteOptions {
	files := map[string]bool{"/models/ship.glb": true, "/tex/a.png": true}
	return staticRewriteOptions{Prefix: prefix, AssetExists: func(rel string) bool { return files[rel] }}
}

func jsonScript(t *testing.T, page, id string) map[string]any {
	t.Helper()
	open := `id="` + id + `"`
	i := strings.Index(page, open)
	if i < 0 {
		t.Fatalf("no script %s in %s", id, page)
	}
	start := strings.Index(page[i:], ">") + i + 1
	end := strings.Index(page[start:], "</script>") + start
	var out map[string]any
	if err := json.Unmarshal([]byte(page[start:end]), &out); err != nil {
		t.Fatalf("decode %s: %v\n%s", id, err, page[start:end])
	}
	return out
}

func TestStaticRewriteMakesJSONContractsAndDataURLsDocumentRelative(t *testing.T) {
	for _, tc := range []struct{ route, up string }{{"/", ""}, {"/play", "../"}, {"/a/b", "../../"}} {
		out, err := rewriteStaticExportHTMLWith(tc.route, staticFixture, fixtureOpts(""))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, `"/gosx/`) {
			t.Fatalf("%s: root-absolute /gosx/ URL left in:\n%s", tc.route, out)
		}
		want := tc.up
		if want == "" {
			want = ""
		}
		for _, snippet := range []string{
			`data-gosx-scene3d-gltf-url="` + want + `gosx/assets/runtime/bootstrap-feature-scene3d-gltf.aa.js"`,
			`src="` + want + `gosx/assets/runtime/bootstrap-feature-scene3d.bb.js"`,
			`"programRef":"` + want + `gosx/assets/islands/Game.11.gxi"`,
			`"runtimePath":"` + want + `gosx/assets/runtime/gosx-runtime-core.cc.wasm"`,
			`"wasmExecPath":"` + want + `gosx/assets/runtime/wasm_exec.ee.js"`,
			`"model":"` + want + `models/ship.glb"`,
			`"texture":"` + want + `tex/a.png?v=2"`,
			`"label":"/not-a-file"`,
			`"pattern":"/play"`,
		} {
			if !strings.Contains(out, snippet) {
				t.Fatalf("%s: missing %q in\n%s", tc.route, snippet, out)
			}
		}
		manifest := jsonScript(t, out, "gosx-manifest")
		if manifest["version"] != "0.1.0" {
			t.Fatalf("manifest not valid JSON after rewrite: %v", manifest)
		}
	}
}

func TestStaticRewriteKeepsExplicitBasePathWorking(t *testing.T) {
	input := strings.NewReplacer("/gosx/", "/games/x/gosx/", `"/models/`, `"/games/x/models/`).Replace(staticFixture)
	out, err := rewriteStaticExportHTMLWith("/games/x/play", input, fixtureOpts("/games/x"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "/games/x/") {
		t.Fatalf("prefixed URL left behind:\n%s", out)
	}
	for _, snippet := range []string{`"programRef":"../gosx/assets/islands/Game.11.gxi"`, `"model":"../models/ship.glb"`, `data-gosx-scene3d-gltf-url="../gosx/assets/runtime/`} {
		if !strings.Contains(out, snippet) {
			t.Fatalf("missing %q in\n%s", snippet, out)
		}
	}
}

func TestStaticRewriteDisablesTelemetryByDefault(t *testing.T) {
	out, err := rewriteStaticExportHTML("/", staticFixture)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<meta name="gosx-telemetry" content="off"/>`) {
		t.Fatalf("missing telemetry-off meta in\n%s", out)
	}
	again, _ := rewriteStaticExportHTML("/", out)
	if strings.Count(again, `name="gosx-telemetry"`) != 1 {
		t.Fatal("telemetry meta was duplicated")
	}
}

func TestStaticHTMLHasNoServerEndpoints(t *testing.T) {
	input := `<html><head><meta property="og:image" content="/_gosx/image?src=%2Fcover.png&w=640"></head><body><img src="/_gosx/image?src=%2Fa.png&w=9"></body></html>`
	out, err := rewriteStaticExportHTML("/", input)
	if err != nil {
		t.Fatal(err)
	}
	if hits := staticServerEndpoints(out); len(hits) != 0 {
		t.Fatalf("static page references server endpoints: %v\n%s", hits, out)
	}
	if hits := staticServerEndpoints(`<form action="/_gosx/csrf"></form>`); len(hits) != 1 {
		t.Fatalf("audit missed endpoint: %v", hits)
	}
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "index.html"), `<a href="/_gosx/live">x</a>`)
	if err := checkStaticServerEndpoints(dir, true); err == nil || !strings.Contains(err.Error(), "/_gosx/live") {
		t.Fatalf("export must fail on an endpoint reference, got %v", err)
	}
	if err := checkStaticServerEndpoints(dir, false); err != nil {
		t.Fatalf("build only warns, got %v", err)
	}
}

func TestDropDormantRuntimeBundles(t *testing.T) {
	noWASM := dropDormantRuntime(staticFixture, routeCapabilities{})
	if strings.Contains(noWASM, "gosx-runtime.dd.wasm") {
		t.Fatal("dormant WASM bundle reference kept for a page with no WASM")
	}
	if !strings.Contains(noWASM, `"bundles":{}`) || !strings.Contains(noWASM, "gosx-runtime-core.cc.wasm") {
		t.Fatalf("bundles emptied wrongly or live runtime path lost:\n%s", noWASM)
	}
	jsonScript(t, noWASM, "gosx-manifest")
	if kept := dropDormantRuntime(staticFixture, routeCapabilities{WASM: true}); kept != staticFixture {
		t.Fatal("page that needs WASM must keep its bundles")
	}
}

func TestWriteStaticZipPutsIndexAtRootWithoutSidecars(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "index.html"), "<p>hi</p>")
	mustWriteFile(t, filepath.Join(dir, "index.html.gz"), "gz")
	mustWriteFile(t, filepath.Join(dir, "gosx", "a.js"), "x")
	zipPath := filepath.Join(t.TempDir(), "out", "game.zip")
	n, err := writeStaticZip(dir, zipPath)
	if err != nil || n != 2 {
		t.Fatalf("zip: n=%d err=%v", n, err)
	}
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var names []string
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "gosx/a.js,index.html" {
		t.Fatalf("archive entries = %v", names)
	}
}

func TestStaticOnlyReportsSkippedPagesInsteadOfDroppingThem(t *testing.T) {
	h := http.Header{}
	h.Set("X-GoSX-Prerender-Reason", "actions")
	if got := dynamicSkipReason(h); !strings.Contains(got, "actions") {
		t.Fatalf("reason = %q", got)
	}
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "[slug]", "page.gsx"), "package app\nfunc Page() Node { return <p>x</p> }\n")
	mustWriteFile(t, filepath.Join(dir, "page.gsx"), "package app\nfunc Page() Node { return <p>x</p> }\n")
	skipped, err := staticOnlySkippedRoutes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "route parameters") {
		t.Fatalf("skipped = %v", skipped)
	}
}
