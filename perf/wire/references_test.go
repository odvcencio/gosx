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
	if err != nil || !set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), []Reference{{URL: "./ready.js", Kind: KindScript, Potential: false}}) {
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
		{URL: "/alternate.mp4", Kind: KindOther, Potential: false}, {URL: "/banner.webp", Kind: KindImage, Potential: false},
		{URL: "/compat.js", Kind: KindScript, Potential: false}, {URL: "/cover.avif", Kind: KindImage, Potential: false},
		{URL: "/first.mp4", Kind: KindOther, Potential: false}, {URL: "/font.woff2", Kind: KindFont, Potential: false},
		{URL: "/gpu.js", Kind: KindScript, Potential: true}, {URL: "/inline.js", Kind: KindScript, Potential: false},
		{URL: "/later.png", Kind: KindImage, Potential: false}, {URL: "/main.js", Kind: KindScript, Potential: false},
		{URL: "/poster.jpg", Kind: KindImage, Potential: false}, {URL: "/prefetched.js", Kind: KindScript, Potential: false}, {URL: "/style.css", Kind: KindStyle, Potential: false}, {URL: "/thumb.png", Kind: KindImage, Potential: false},
	}
	if !reflect.DeepEqual(referenceValues(set.Resources), want) {
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
	want := []Reference{{URL: "/compute.bin", Kind: KindProgram, Potential: false}, {URL: "/counter.bin", Kind: KindProgram, Potential: false}, {URL: "/custom.wasm", Kind: KindWASM, Potential: false}, {URL: "/selected.wasm", Kind: KindWASM, Potential: false}}
	if err != nil || !set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), want) {
		t.Fatalf("wrong selected runtime: %#v %v", set, err)
	}
	legacy := []byte(`<script id="gosx-manifest" type="application/json">{"version":"0.1.0","islands":[{"bundleId":"active"}],"runtime":{"path":"/core.wasm"},"bundles":{"active":{"path":"/app.wasm"},"other":{"path":"/full.wasm"}}}</script>`)
	set, err = ScanReferences(legacy, KindDocument)
	want = []Reference{{URL: "/app.wasm", Kind: KindWASM, Potential: false}, {URL: "/core.wasm", Kind: KindWASM, Potential: false}}
	if err != nil || !set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), want) {
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
	want := []Reference{{URL: "../fonts/a.woff2", Kind: KindFont, Potential: false}, {URL: "./base.css", Kind: KindStyle, Potential: false}, {URL: "./large.webp", Kind: KindImage, Potential: false}, {URL: "./print.css", Kind: KindStyle, Potential: false}, {URL: "./small.webp", Kind: KindImage, Potential: false}, {URL: "/cover.png", Kind: KindImage, Potential: false}}
	if err != nil || !set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), want) {
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
	want := []Reference{{URL: "./all.js", Kind: KindScript, Potential: false}, {URL: "./entry.js", Kind: KindScript, Potential: false}, {URL: "./export.js", Kind: KindScript, Potential: false}, {URL: "./font.woff2", Kind: KindFont, Potential: false}, {URL: "./lazy.js", Kind: KindScript, Potential: false}, {URL: "./mesh.glb", Kind: KindOther, Potential: false}, {URL: "./shared.js", Kind: KindScript, Potential: false}, {URL: "./side-effect.js", Kind: KindScript, Potential: false}, {URL: "./worker.js", Kind: KindScript, Potential: false}}
	if err != nil || !set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), want) {
		t.Fatalf("module references differ: %#v %v", set, err)
	}
}

func TestReferencesWrappedURLsPreserveContextKinds(t *testing.T) {
	for _, tc := range []struct {
		name, kind, extension, direct, wrapped string
	}{
		{"stylesheet", KindStyle, ".css", `@import "TARGET";`, `@import url("TARGET");`},
		{"worker", KindScript, ".js", `new Worker("TARGET");`, `new Worker(new URL("TARGET", import.meta.url));`},
		{"shared-worker", KindScript, ".js", `new SharedWorker("TARGET");`, `new SharedWorker(new URL("TARGET", import.meta.url));`},
	} {
		for _, target := range []string{"./dependency", "./dependency#x", "./dependency" + tc.extension + "#x"} {
			t.Run(tc.name+"/"+target, func(t *testing.T) {
				want := ReferenceSet{Resources: []Reference{{URL: target, Kind: tc.kind, Potential: false}}, Complete: true}
				for _, template := range []string{tc.direct, tc.wrapped} {
					set, err := ScanReferences([]byte(strings.ReplaceAll(template, "TARGET", target)), tc.kind)
					if err != nil || !set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), want.Resources) {
						t.Errorf("direct and wrapped references must preserve context kind: %#v %v", set, err)
					}
				}
			})
		}
	}
}

func TestReferencesURLKindsStayWithinLoadContext(t *testing.T) {
	for _, tc := range []struct {
		name, kind, body string
		want             []Reference
	}{
		{"background", KindStyle, `.a { background: url("./resource"); }`, []Reference{{URL: "./resource", Kind: KindOther, Potential: false}}},
		{"standalone", KindScript, `new URL("./resource", import.meta.url);`, []Reference{{URL: "./resource", Kind: KindOther, Potential: false}}},
		{"worker-options", KindScript, `new Worker("./worker", {resource: new URL("./resource", import.meta.url)});`, []Reference{{URL: "./resource", Kind: KindOther, Potential: false}, {URL: "./worker", Kind: KindScript, Potential: false}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set, err := ScanReferences([]byte(tc.body), tc.kind)
			if err != nil || !set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), tc.want) {
				t.Fatal("URL outside a typed load context gained a kind", set, err)
			}
		})
	}
}

func TestReferencesUnresolvedSyntaxDoesNotProveClosure(t *testing.T) {
	tests := []struct{ name, kind, body string }{
		{"computed-import", KindScript, "import(selected)"},
		{"concatenated-import", KindScript, "import('./'+selected+'.js')"},
		{"computed-fetch", KindScript, "fetch(selected)"},
		{"member-fetch", KindScript, "globalThis.fetch(selected)"},
		{"aliased-fetch", KindScript, "const request=fetch; request(selected)"},
		{"aliased-member-fetch", KindScript, `const request=globalThis.fetch; request("/mesh.glb")`},
		{"destructured-fetch", KindScript, `const {fetch: request}=globalThis; request("/mesh.glb")`},
		{"xhr", KindScript, "new XMLHttpRequest()"},
		{"worker", KindScript, "new Worker(selected)"},
		{"worker-unknown-constructor", KindScript, "new Worker(new Date())"},
		{"worker-url-base", KindScript, "new Worker(new URL('./worker',base))"},
		{"url-base", KindScript, "new URL('./model.glb',base)"},
		{"single-quote-escape", KindScript, "import('./\\u0061.js')"},
		{"static-import-escape", KindScript, `import './\u0061.js';`},
		{"export-escape", KindScript, `export * from './\u0061.js';`},
		{"eval", KindScript, "eval(source)"},
		{"function", KindScript, "new Function(source)"},
		{"css-escaped-url", KindStyle, `.a{background:u\72l("/image.png")}`},
		{"css-escaped-import", KindStyle, `@\69mport "/sheet.css";`},
		{"css-escaped-image-set", KindStyle, `.a{background:\69mage-set("/image.png" 1x)}`},
		{"responsive-image", KindDocument, `<img src="/a.png" srcset="/a.png 1x,/b.png 2x">`},
		{"import-map", KindDocument, `<script type="importmap">{"imports":{}}</script>`},
		{"duplicate-attribute", KindDocument, `<script src="/first.js" src="/second.js"></script>`},
		{"missing-runtime", KindDocument, `<script type="application/json" id="gosx-manifest">{"version":"0.1.0","islands":[{"programRef":"/a.bin"}]}</script>`},
		{"missing-bundle", KindDocument, `<script type="application/json" id="gosx-manifest">{"version":"0.1.0","islands":[{"bundleId":"absent"}],"runtime":{"path":"/core.wasm"}}</script>`},
		{"unknown-manifest", KindDocument, `<script type="application/json" id="gosx-manifest">{"version":"future","bundles":{}}</script>`},
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

func TestReferencesLoaderEscapesAreIncomplete(t *testing.T) {
	for _, loader := range []string{
		"fetch", "Worker", "SharedWorker", "URL", "importScripts",
		"EventSource", "WebSocket", "XMLHttpRequest", "eval", "Function",
	} {
		for _, receiver := range []string{"globalThis", "window", "self"} {
			member := receiver + "." + loader
			for _, tc := range []struct{ name, body string }{
				{"assigned", `const request=MEMBER; request("/resource")`},
				{"argument", `consume(MEMBER)`},
				{"returned", `function loader(){return MEMBER}`},
				{"bound", `const request=MEMBER.bind(RECEIVER); request("/resource")`},
				{"call", `MEMBER.call(RECEIVER,"/resource")`},
				{"apply", `MEMBER.apply(RECEIVER,["/resource"])`},
				{"destructured", `const {LOADER: request}=RECEIVER; request("/resource")`},
				{"quoted-destructured", `const {"LOADER": request}=RECEIVER; request("/resource")`},
				{"computed", `const request=RECEIVER[COMPUTED]; request("/resource")`},
				{"computed-call", `RECEIVER[COMPUTED]("/resource")`},
				{"computed-destructured", `const {[COMPUTED]: request}=RECEIVER; request("/resource")`},
			} {
				t.Run(loader+"/"+receiver+"/"+tc.name, func(t *testing.T) {
					computed := `"` + loader[:1] + `"+"` + loader[1:] + `"`
					body := strings.NewReplacer("MEMBER", member, "RECEIVER", receiver, "LOADER", loader, "COMPUTED", computed).Replace(tc.body)
					set, err := ScanReferences([]byte(body), KindScript)
					if err != nil || set.Complete {
						t.Fatal("escaped loader claimed complete coverage", set, err)
					}
				})
			}
		}
		t.Run(loader+"/bare-alias", func(t *testing.T) {
			set, err := ScanReferences([]byte(`const request=`+loader+`; request("/resource")`), KindScript)
			if err != nil || set.Complete {
				t.Fatal("bare loader alias claimed complete coverage", set, err)
			}
		})
		t.Run(loader+"/shorthand-destructured", func(t *testing.T) {
			set, err := ScanReferences([]byte(`const {`+loader+`}=globalThis; `+loader+`("/resource")`), KindScript)
			if err != nil || set.Complete {
				t.Fatal("shorthand loader binding claimed complete coverage", set, err)
			}
		})
	}
}

func TestReferencesGlobalLoaderCalls(t *testing.T) {
	for _, receiver := range []string{"", "globalThis.", "window.", "self."} {
		for _, tc := range []struct{ loader, body, kind string }{
			{"fetch", `RECEIVERfetch("./resource")`, KindOther},
			{"Worker", `new RECEIVERWorker("./resource")`, KindScript},
			{"SharedWorker", `new RECEIVERSharedWorker(new RECEIVERURL("./resource",import.meta.url))`, KindScript},
			{"URL", `new RECEIVERURL("./resource",import.meta.url)`, KindOther},
		} {
			t.Run(receiver+tc.loader, func(t *testing.T) {
				body := strings.ReplaceAll(tc.body, "RECEIVER", receiver)
				set, err := ScanReferences([]byte(body), KindScript)
				want := ReferenceSet{Resources: []Reference{{URL: "./resource", Kind: tc.kind, Potential: false}}, Complete: true}
				if err != nil || !set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), want.Resources) {
					t.Fatal("direct global loader lost its reference", set, err)
				}
			})
		}
	}
}

func TestReferencesUnsupportedLoaderFormsAreIncomplete(t *testing.T) {
	for _, body := range []string{
		`new self.XMLHttpRequest()`, `xhr.open("GET","/resource")`,
		`const request=xhr.open; request("GET","/resource")`, `const {open: request}=xhr`,
		`navigator.serviceWorker.register("/worker.js")`, `window.navigator.serviceWorker.register("/worker.js")`,
		`const register=navigator.serviceWorker.register; register("/worker.js")`,
		`const {register}=navigator.serviceWorker; register("/worker.js")`,
		`const {register: install}=sw; install("/worker.js")`,
		`const {serviceWorker: sw}=navigator; sw.register("/worker.js")`,
		`const sw=navigator.serviceWorker; sw.register("/worker.js")`,
		`navigator["service"+"Worker"]["reg"+"ister"]("/worker.js")`,
		`const request=globalThis.f\u0065tch; request("/resource")`,
		`const {f\u0065tch: request}=window; request("/resource")`,
		`const {"f\u0065tch": request}=self; request("/resource")`,
		`const request=Reflect.get(globalThis,"fetch"); request("/resource")`,
		`const lookup=window.Reflect.get; const request=lookup(self,"fetch"); request("/resource")`,
		`const request=Object.getOwnPropertyDescriptor(window,"fetch").value; request("/resource")`,
		`const lookup=Object.getOwnPropertyDescriptor; const request=lookup(self,"fetch").value; request("/resource")`,
		`const {getOwnPropertyDescriptor: lookup}=Object; lookup(globalThis,"fetch").value("/resource")`,
		`const descriptors=Object.getOwnPropertyDescriptors(window); consume(descriptors)`,
		`window.open("/document")`, `const navigate=window.open; navigate("/document")`,
	} {
		t.Run(body, func(t *testing.T) {
			set, err := ScanReferences([]byte(body), KindScript)
			if err != nil || set.Complete {
				t.Fatal("unsupported loader claimed complete coverage", set, err)
			}
		})
	}
	for _, tc := range []struct{ name, kind, body string }{
		{"dynamic-import", KindScript, `import(selected)`},
		{"computed-callee", KindScript, `const root=globalThis; root["fe"+"tch"]("/resource")`},
		{"computed-property", KindScript, `const { [selected]: request }=globalThis; request("/resource")`},
		{"css-url", KindStyle, `.a{background:url(var(--asset))}`},
		{"css-import", KindStyle, `@import url(var(--sheet));`},
		{"css-import-function", KindStyle, `@import var(--sheet);`},
		{"css-image-set", KindStyle, `.a{background:image-set(var(--asset) 1x)}`},
		{"css-webkit-image-set", KindStyle, `.a{background:-webkit-image-set(var(--asset) 1x)}`},
		{"css-variable-source", KindStyle, `.a{background-image:var(--asset)}`},
		{"css-font-variable", KindStyle, `@font-face{font-family:Fixture;src:var(--font)}`},
		{"html-link", KindDocument, `<link rel="preload modulepreload" href="/a.js" href="/b.js">`},
		{"html-script", KindDocument, `<script>const request=window.fetch; request("/resource")</script>`},
		{"html-media", KindDocument, `<source src="/a.webp" srcset="/a.webp 1x,/b.webp 2x">`},
		{"html-object", KindDocument, `<object data="/a" data="/b"></object>`},
		{"html-hint", KindDocument, `<div data-gosx-scene3d-url="/invalid path"></div>`},
		{"html-manifest", KindDocument, `<script type="application/json" id="gosx-manifest">{"version":"0.1.0","islands":[{"bundleId":"unknown"}]}</script>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set, err := ScanReferences([]byte(tc.body), tc.kind)
			if err != nil || set.Complete {
				t.Fatal("unresolved resource syntax claimed complete coverage", set, err)
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
		{KindDocument, []byte{0xff}},
		{KindFont, []byte("unused")},
	}
	for i, test := range cases {
		set, err := ScanReferences(test.body, test.kind)
		var typed *ReferenceError
		if set.Complete || !errors.As(err, &typed) || typed.Code != "invalid-input" || typed.Reference != "references" || typed.Pointer != "/body" {
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
	body := []byte(`<script type="application/json" id="gosx-manifest">{"version":"0.1.0","runtime":{"path":"/gosx/core.wasm"},"bundles":{"dormant":{"path":"/gosx/full.wasm"}}}</script>`)
	set, err := ScanReferences(body, KindDocument)
	if err != nil || !set.Complete || len(set.Resources) != 0 {
		t.Fatal("new scanner did not distinguish inventory from selected runtime", set, err)
	}
}

func TestReferencesHydrationRuntimeConsumerSelection(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		selected     bool
		conditional  bool
	}{
		{"unused-runtime", `"islands":[],"engines":[]`, false, false},
		{"island-selected", `"islands":[{"programRef":"/counter.bin"}]`, true, false},
		{"island-not-selected", `"islands":[]`, false, false},
		{"static-island-selects-runtime", `"islands":[{"static":true,"programRef":"/static.bin"}]`, true, false},
		{"compute-selected", `"computeIslands":[{"programRef":"/compute.bin"}]`, true, false},
		{"compute-not-selected", `"computeIslands":[]`, false, false},
		{"shared-engine-selected", `"engines":[{"runtime":"shared","programRef":"/engine.bin"}]`, true, false},
		{"shared-engine-not-selected", `"engines":[{"runtime":"js"}]`, false, false},
		{"shared-engine-exact-token", `"engines":[{"runtime":"Shared"}]`, false, false},
		{"go-wasm-is-separate", `"engines":[{"runtime":"go-wasm","programRef":"/custom.wasm"}]`, false, false},
		{"hub-selected-by-monolith", `"hubs":[{}]`, true, true},
		{"hub-not-selected", `"hubs":[]`, false, false},
		{"identity-selected-by-monolith", `"clientIdentity":{}`, true, true},
		{"identity-not-selected", `"clientIdentity":null`, false, false},
		{"video-selected-by-monolith", `"engines":[{"kind":"video"}]`, true, true},
		{"video-not-selected", `"engines":[{"kind":"surface"}]`, false, false},
		{"video-exact-token", `"engines":[{"kind":"Video"}]`, false, false},
		{"keyboard-selected-by-monolith", `"engines":[{"capabilities":["keyboard"]}]`, true, true},
		{"keyboard-not-selected", `"engines":[{"capabilities":["Keyboard"]}]`, false, false},
		{"pointer-selected-by-monolith", `"engines":[{"capabilities":["pointer"]}]`, true, true},
		{"pointer-not-selected", `"engines":[{"capabilities":[" pointer "]}]`, false, false},
		{"gamepad-selected-by-monolith", `"engines":[{"capabilities":["gamepad"]}]`, true, true},
		{"gamepad-not-selected", `"engines":[{"capabilities":["Gamepad"]}]`, false, false},
		{"required-capabilities-are-separate", `"engines":[{"requiredCapabilities":["keyboard","pointer","gamepad"]}]`, false, false},
		{"controller-is-not-a-consumer", `"controllers":[{}]`, false, false},
		{"bundle-inventory-is-not-a-consumer", `"bundles":{"unused":{"path":"/other.wasm"}}`, false, false},
		{"common-gate-wins-over-bridge", `"islands":[{"programRef":"/counter.bin"}],"hubs":[{}]`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, runtime := range []string{"/runtime.wasm", ""} {
				body := []byte(`<script type="application/json" id="gosx-manifest">{"version":"0.1.0","runtime":{"path":"` + runtime + `"},` + tc.fields + `}</script>`)
				set, err := ScanReferences(body, KindDocument)
				if err != nil {
					t.Fatal(err)
				}
				wantComplete := !tc.selected || runtime != "" && !tc.conditional
				if set.Complete != wantComplete {
					t.Fatalf("runtime=%q completeness=%v want %v", runtime, set.Complete, wantComplete)
				}
				found := false
				for _, ref := range set.Resources {
					if ref.URL == "/runtime.wasm" {
						found = true
						if ref.Kind != KindWASM || ref.Potential != tc.conditional {
							t.Fatal("runtime selection certainty differs", ref)
						}
					}
					if ref.URL == "/static.bin" || ref.URL == "/other.wasm" {
						t.Fatal("unselected program or inventory was scanned", ref)
					}
				}
				if found != (tc.selected && runtime != "") {
					t.Fatalf("runtime=%q found=%v want %v", runtime, found, tc.selected && runtime != "")
				}
			}
		})
	}
}

func TestReferencesGlobalNonLoadingCalls(t *testing.T) {
	for _, global := range []string{"window", "self", "globalThis"} {
		t.Run(global, func(t *testing.T) {
			body := global + `.addEventListener("focus", () => fetch("/visible.json"));`
			set, err := ScanReferences([]byte(body), KindScript)
			want := []Reference{{URL: "/visible.json", Kind: KindOther}}
			if err != nil || !set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), want) {
				t.Fatalf("non-loading call became a resource: %+v err=%v", set, err)
			}
		})
	}
}

// Existing URL/kind assertions remain independent of the context contract.
func referenceValues(refs []Reference) []Reference {
	out := append([]Reference{}, refs...)
	for i := range out {
		out[i].Base = ""
		out[i].Worker = false
	}
	return out
}
