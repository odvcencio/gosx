package main

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"os"
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

func TestStaticRewriteLeavesJSONKeysAndTextureVariantsAlone(t *testing.T) {
	opts := staticRewriteOptions{AssetExists: func(rel string) bool { return rel == "/models/ship.png" || rel == "/models/ship.ktx2" }}
	got := rewriteStaticJSON("/", `{"textureVariants": {"/models/ship.png":[{"uri":"/models/ship.ktx2"}]},"model":"/models/ship.png"}`, opts)
	want := `{"textureVariants": {"/models/ship.png":[{"uri":"/models/ship.ktx2"}]},"model":"models/ship.png"}`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
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
	if plain, _ := rewriteStaticExportHTML("/", staticFixture); strings.Contains(plain, "gosx-telemetry") {
		t.Fatal("a hybrid build keeps telemetry on")
	}
	off := staticRewriteOptions{TelemetryOff: true}
	out, err := rewriteStaticExportHTMLWith("/", staticFixture, off)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<meta name="gosx-telemetry" content="off"/>`) {
		t.Fatalf("missing telemetry-off meta in\n%s", out)
	}
	again, _ := rewriteStaticExportHTMLWith("/", out, off)
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
	if hits := staticServerEndpoints(out, nil); len(hits) != 0 {
		t.Fatalf("static page references server endpoints: %v\n%s", hits, out)
	}
	if hits := staticServerEndpoints(`<form action="/_gosx/csrf"></form>`, nil); len(hits) != 1 {
		t.Fatalf("audit missed endpoint: %v", hits)
	}
	if hits := staticServerEndpoints(`<p>the optimizer lives at <code>/_gosx/image</code></p>`, nil); len(hits) != 0 {
		t.Fatalf("page text is not a server call: %v", hits)
	}
	staged := func(p string) bool { return p == "/_gosx/docs-css/a.css" }
	if hits := staticServerEndpoints(`<link href="../_gosx/docs-css/a.css"><b data-gosx-css-source="/_gosx/docs-css/a.css?scope=1">x</b>`, staged); len(hits) != 0 {
		t.Fatalf("staged file flagged: %v", hits)
	}
	if hits := staticServerEndpoints(`<script id="gosx-manifest" type="application/json">{"a":"/_gosx/live"}</script>`, nil); len(hits) != 1 {
		t.Fatalf("JSON contract endpoint missed: %v", hits)
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

func TestResolveWASMOptFallsBackToTinyGoBundle(t *testing.T) {
	root := t.TempDir()
	bundled := filepath.Join(root, "bin", "wasm-opt")
	mustWriteFile(t, bundled, "#!/bin/sh\n")
	if err := os.Chmod(bundled, 0755); err != nil {
		t.Fatal(err)
	}
	missing := func(string) (string, error) { return "", os.ErrNotExist }
	withTinyGo := func(name string) (string, error) {
		if name == "tinygo" {
			return filepath.Join(root, "nowhere", "tinygo"), nil
		}
		return "", os.ErrNotExist
	}
	rootOf := func(string) string { return root }
	if got := resolveWASMOpt(func(n string) (string, error) { return "/usr/bin/" + n, nil }, rootOf); got != "/usr/bin/wasm-opt" {
		t.Fatalf("PATH wasm-opt must win, got %q", got)
	}
	if got := resolveWASMOpt(withTinyGo, rootOf); got != bundled {
		t.Fatalf("bundled = %q, want %q", got, bundled)
	}
	if got := resolveWASMOpt(withTinyGo, func(string) string { return "" }); got != "" {
		t.Fatalf("no root, no sibling lib dir: got %q", got)
	}
	if got := resolveWASMOpt(missing, rootOf); got != "" {
		t.Fatalf("no tinygo: got %q", got)
	}
}

func TestRewriteStaticCSSMakesURLsRelativeToTheStylesheet(t *testing.T) {
	files := map[string]bool{"/img/bg.png": true, "/fonts/a.woff2": true}
	opts := staticRewriteOptions{AssetExists: func(rel string) bool { return files[rel] }}
	src := `a{background:url(/img/bg.png)} b{background:url( "/img/bg.png?v=2#x" )} @font-face{src:url('/fonts/a.woff2')} c{background:url(/gosx/assets/css/x.png)} d{background:url(/missing.png)} e{background:url(//cdn.example/x.png)} f{background:url(data:image/png;base64,AAAA)} @import "/gosx/assets/css/base.css";`
	root := rewriteStaticCSS("styles.css", src, opts)
	for _, want := range []string{`url(img/bg.png)`, `url( "img/bg.png?v=2#x" )`, `url('fonts/a.woff2')`, `url(gosx/assets/css/x.png)`, `url(/missing.png)`, `url(//cdn.example/x.png)`, `url(data:image/png;base64,AAAA)`, `@import "gosx/assets/css/base.css"`} {
		if !strings.Contains(root, want) {
			t.Fatalf("missing %q in %s", want, root)
		}
	}
	nested := rewriteStaticCSS("gosx/assets/css/app.css", `a{background:url(/img/bg.png)}`, opts)
	if nested != `a{background:url(../../../img/bg.png)}` {
		t.Fatalf("nested = %s", nested)
	}
	prefixed := rewriteStaticCSS("styles.css", `a{background:url(/games/x/img/bg.png)}`, staticRewriteOptions{Prefix: "/games/x", AssetExists: opts.AssetExists})
	if prefixed != `a{background:url(img/bg.png)}` {
		t.Fatalf("prefixed = %s", prefixed)
	}
}

func TestRewriteStaticCSSTreeDropsStaleSidecars(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "css", "a.css"), `a{background:url(/gosx/x.png)}`)
	mustWriteFile(t, filepath.Join(dir, "css", "a.css.gz"), "stale")
	mustWriteFile(t, filepath.Join(dir, "css", "b.css"), `b{color:red}`)
	mustWriteFile(t, filepath.Join(dir, "css", "b.css.gz"), "keep")
	if err := rewriteStaticCSSTree(dir, staticRewriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "css", "a.css")); got != `a{background:url(../gosx/x.png)}` {
		t.Fatalf("a.css = %s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "css", "a.css.gz")); !os.IsNotExist(err) {
		t.Fatal("stale sidecar kept")
	}
	if _, err := os.Stat(filepath.Join(dir, "css", "b.css.gz")); err != nil {
		t.Fatal("untouched sidecar removed")
	}
}
