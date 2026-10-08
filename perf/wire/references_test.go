package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestReferencesProductionEngineChunk(t *testing.T) {
	body, err := os.ReadFile("../../client/js/bootstrap-feature-engines.js")
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte{}, body...)
	set, err := ScanReferences(body, KindScript)
	if err != nil || set.Complete || !bytes.Equal(body, before) {
		t.Fatal("production code must retain computed-fetch uncertainty", err)
	}
}

func TestReferencesFormattedMinifiedStatementBoundaries(t *testing.T) {
	body := []byte(`(function(){if(ready)try{mount()}catch{ready=false}if(ready){let a=true;a&&(ready=false)}import("./ready.js")})();`)
	set, err := ScanReferences(body, KindScript)
	if err != nil || !set.Complete || !reflect.DeepEqual(set.Resources, []Reference{{"./ready.js", KindScript, false}}) {
		t.Fatal("minified statements changed module extraction", set, err)
	}
}

func TestReferencesHTMLActiveRootsAndPotentialChunks(t *testing.T) {
	body := []byte(`<!doctype html><head>
<link rel="stylesheet" href="/style.css"><link rel="modulepreload" href="/main.js">
<link rel="preload" as="font" href="/font.woff2"><link rel="preload" as="image" href="/cover.avif"><link rel="prefetch" href="/prefetched.js">
<script defer src="/main.js"></script><script type="application/json" src="/data.js">{"asset":"/not-loaded.js"}</script>
<script type="text/ecmascript" src="/compat.js"></script><script>import("/inline.js")</script>
<style>.banner{background:url("/banner.webp")}</style></head>
<body data-gosx-scene3d-webgpu-url="/gpu.js" data-gosx-unused-url="/main.js">
<template><script src="/inert.js"></script><img src="/inert.png"></template>
<img src="/later.png" loading="lazy" style="background:url('/thumb.png')">
<video src="/first.mp4" poster="/poster.jpg"><source src="/alternate.mp4"></video>
<img src="data:image/png;base64,AAAA"></body>`)
	set, err := ScanReferences(body, KindDocument)
	if err != nil || !set.Complete {
		t.Fatal("active markup did not resolve", err)
	}
	want := []Reference{
		{"/alternate.mp4", KindOther, false}, {"/banner.webp", KindImage, false},
		{"/compat.js", KindScript, false}, {"/cover.avif", KindImage, false},
		{"/first.mp4", KindOther, false}, {"/font.woff2", KindFont, false},
		{"/gpu.js", KindScript, true}, {"/inline.js", KindScript, false},
		{"/later.png", KindImage, false}, {"/main.js", KindScript, false},
		{"/poster.jpg", KindImage, false}, {"/prefetched.js", KindScript, false}, {"/style.css", KindStyle, false}, {"/thumb.png", KindImage, false},
	}
	if !reflect.DeepEqual(set.Resources, want) {
		t.Fatalf("active roots or conditional hints differ: %#v", set.Resources)
	}
}

func TestReferencesHydrationSelectionExcludesDormantBundles(t *testing.T) {
	body := []byte(`<script type="application/json" id="gosx-manifest">{
"version":"0.1.0","runtime":{"path":"/selected.wasm"},
"islands":[{"bundleId":"unused","programRef":"/counter.bin"},{"static":true,"programRef":"/static.bin"}],
"computeIslands":[{"programRef":"/compute.bin"}],
"engines":[{"runtime":"go-wasm","programRef":"/custom.wasm"}],
"bundles":{"unused":{"path":"/full.wasm"},"dormant":{"path":"/other.wasm"}}
}</script>`)
	set, err := ScanReferences(body, KindDocument)
	want := []Reference{{"/compute.bin", KindProgram, false}, {"/counter.bin", KindProgram, false}, {"/custom.wasm", KindWASM, false}, {"/selected.wasm", KindWASM, false}}
	if err != nil || !set.Complete || !reflect.DeepEqual(set.Resources, want) {
		t.Fatalf("wrong selected runtime: %#v %v", set, err)
	}
	legacy := []byte(`<script id="gosx-manifest" type="application/json">{"version":"0.1.0","islands":[{"bundleId":"active"}],"runtime":{"path":"/core.wasm"},"bundles":{"active":{"path":"/app.wasm"},"other":{"path":"/full.wasm"}}}</script>`)
	set, err = ScanReferences(legacy, KindDocument)
	want = []Reference{{"/app.wasm", KindWASM, false}, {"/core.wasm", KindWASM, false}}
	if err != nil || !set.Complete || !reflect.DeepEqual(set.Resources, want) {
		t.Fatal("active legacy bundle was omitted or inventory was charged", set, err)
	}
}

func TestReferencesCSSImportsFontsImagesAndImageSets(t *testing.T) {
	body := []byte(`/* url("/comment.png") */ @import "./base.css";
@import url("./print.css") print;
@font-face{font-family:"url('/fake.woff2')";src:local("Fixture"),url("../fonts/a.woff2") format("woff2")}
.banner{background:URL(/cover.png);mask:url("#mask");content:"url('/fake.png')"}
.hero{background:image-set("./small.webp" 1x,url("./large.webp") 2x)}
.inline{background:url(data:image/png;base64,AAAA)}
`)
	set, err := ScanReferences(body, KindStyle)
	want := []Reference{{"../fonts/a.woff2", KindFont, false}, {"./base.css", KindStyle, false}, {"./large.webp", KindImage, false}, {"./print.css", KindStyle, false}, {"./small.webp", KindImage, false}, {"/cover.png", KindImage, false}}
	if err != nil || !set.Complete || !reflect.DeepEqual(set.Resources, want) {
		t.Fatalf("CSS dependency closure differs: %#v %v", set, err)
	}
}

func TestReferencesModulesUseSyntaxAndKeepLiteralLazyImports(t *testing.T) {
	body := []byte(`import x from "./entry.js";
import "./side-effect.js";
export {x} from "./export.js"; export * from "./all.js"; export const count=1;
import("./lazy.js"); fetch("./mesh.glb");
new Worker(new URL("./worker.js",import.meta.url));
new SharedWorker("./shared.js"); new URL("./font.woff2",import.meta.url);
const text="import('/string.js')"; const regex=/import("regex.js")/;
// import("/comment.js")
/* fetch("/comment.glb") */
`)
	set, err := ScanReferences(body, KindScript)
	want := []Reference{{"./all.js", KindScript, false}, {"./entry.js", KindScript, false}, {"./export.js", KindScript, false}, {"./font.woff2", KindFont, false}, {"./lazy.js", KindScript, false}, {"./mesh.glb", KindOther, false}, {"./shared.js", KindScript, false}, {"./side-effect.js", KindScript, false}, {"./worker.js", KindScript, false}}
	if err != nil || !set.Complete || !reflect.DeepEqual(set.Resources, want) {
		t.Fatalf("module references differ: %#v %v", set, err)
	}
}

func TestReferencesUnresolvedSyntaxDoesNotProveClosure(t *testing.T) {
	tests := []struct{ name, kind, body string }{
		{"computed-import", KindScript, "import(selected)"},
		{"concatenated-import", KindScript, "import('./'+selected+'.js')"},
		{"computed-fetch", KindScript, "fetch(selected)"},
		{"member-fetch", KindScript, "globalThis.fetch(selected)"},
		{"aliased-fetch", KindScript, "const request=fetch; request(selected)"},
		{"xhr", KindScript, "new XMLHttpRequest()"},
		{"worker", KindScript, "new Worker(selected)"},
		{"worker-unknown-constructor", KindScript, "new Worker(new Date())"},
		{"url-base", KindScript, "new URL('./model.glb',base)"},
		{"single-quote-escape", KindScript, "import('./\\u0061.js')"},
		{"eval", KindScript, "eval(source)"},
		{"function", KindScript, "new Function(source)"},
		{"responsive-image", KindDocument, `<img src="/a.png" srcset="/a.png 1x,/b.png 2x">`},
		{"base", KindDocument, `<base href="/other/"><script src="main.js"></script>`},
		{"import-map", KindDocument, `<script type="importmap">{"imports":{}}</script>`},
		{"duplicate-attribute", KindDocument, `<script src="/first.js" src="/second.js"></script>`},
		{"missing-runtime", KindDocument, `<script id="gosx-manifest">{"version":"0.1.0","islands":[{"programRef":"/a.bin"}]}</script>`},
		{"missing-bundle", KindDocument, `<script id="gosx-manifest">{"version":"0.1.0","islands":[{"bundleId":"absent"}],"runtime":{"path":"/core.wasm"}}</script>`},
		{"unknown-manifest", KindDocument, `<script id="gosx-manifest">{"version":"future","bundles":{}}</script>`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set, err := ScanReferences([]byte(test.body), test.kind)
			if err == nil && set.Complete {
				t.Fatal("unresolved references established complete coverage")
			}
		})
	}
}

func TestReferencesInvalidInputsReturnFixedError(t *testing.T) {
	cases := []struct {
		kind string
		body []byte
	}{
		{KindScript, []byte("import './unterminated")},
		{KindStyle, []byte(".a{background:url('unfinished)")},
		{KindDocument, []byte(`<script id="gosx-manifest">{invalid}</script>`)},
		{KindDocument, []byte(`<script id="gosx-manifest">{}</script><script id="gosx-manifest">{}</script>`)},
		{KindDocument, []byte{0xff}},
		{KindScript, bytes.Repeat([]byte(" "), 16<<20+1)},
		{KindFont, []byte("unused")},
		{KindDocument, []byte(strings.Repeat("<div>", 258) + strings.Repeat("</div>", 258))},
	}
	for i, test := range cases {
		_, err := ScanReferences(test.body, test.kind)
		var typed *ReferenceError
		if !errors.As(err, &typed) || typed.Code != "invalid-input" || typed.Reference != "references" || typed.Pointer != "/body" {
			t.Fatalf("case %d lost fixed error: %v", i, err)
		}
	}
}

func TestReferencesParallelScansDoNotShareResults(t *testing.T) {
	var group sync.WaitGroup
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			set, err := ScanReferences([]byte("import './unique.js';"), KindScript)
			if err != nil || !set.Complete || len(set.Resources) != 1 || set.Resources[0].URL != "./unique.js" {
				t.Error("concurrent parser result changed", set, err)
			}
			if len(set.Resources) > 0 {
				set.Resources[0].URL = "mutated"
			}
		}()
	}
	group.Wait()
}

func TestReferencesDoNotChangeCompatibilityCrawlerContract(t *testing.T) {
	// The old crawler continues to charge every advertised bundle. New
	// closure consumers must opt into ScanReferences independently.
	manifest := map[string]any{"runtime": map[string]any{"path": "/gosx/core.wasm"}, "bundles": map[string]any{"dormant": map[string]any{"path": "/gosx/full.wasm"}}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	old := manifestRefs(string(raw))
	if len(old) != 2 {
		t.Fatal("legacy manifest accounting changed", old)
	}
	body := []byte(`<script id="gosx-manifest">{"version":"0.1.0","runtime":{"path":"/gosx/core.wasm"},"bundles":{"dormant":{"path":"/gosx/full.wasm"}}}</script>`)
	set, err := ScanReferences(body, KindDocument)
	if err != nil || len(set.Resources) != 1 || set.Resources[0].URL != "/gosx/core.wasm" {
		t.Fatal("new scanner did not distinguish inventory from selected runtime", set, err)
	}
}
