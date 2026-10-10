package wire

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

type loaderInventoryWitness struct {
	Source, Kind string
	Complete     bool
	References   []Reference
}

var loaderExecutableContexts = []string{
	"script", "inline", "handler", "javascript-url", "srcdoc", "srcdoc-handler",
	"srcdoc-javascript-url", "fetched-script", "fetched-handler", "fetched-javascript-url",
}

// Fetch controls use the real document edge and a served child, rather than
// assuming that the root and fetched-document routes have the same behavior.
func scanLoaderInventoryContext(t *testing.T, source, context string) (ReferenceSet, error) {
	t.Helper()
	if context == "srcdoc-javascript-url" {
		body := `<iframe srcdoc="` + html.EscapeString(executableAttributeDocument("javascript-url", source)) + `"></iframe>`
		return ScanReferences([]byte(body), KindDocument)
	}
	if strings.HasPrefix(context, "fetched-") {
		body := executableAttributeDocument(strings.TrimPrefix(context, "fetched-"), source)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			if r.URL.Path == "/" {
				fmt.Fprint(w, `<iframe src="/child"></iframe>`)
			} else {
				fmt.Fprint(w, body)
			}
		}))
		defer server.Close()
		read := func(path string) []byte {
			r, err := server.Client().Get(server.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Body.Close()
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			return raw
		}
		root, err := ScanReferences(read("/"), KindDocument)
		if err != nil || !root.Complete || !reflect.DeepEqual(root.Resources, []Reference{{"/child", KindDocument, false}}) {
			t.Fatalf("child edge lost: %+v err=%v", root, err)
		}
		return ScanReferences(read(root.Resources[0].URL), KindDocument)
	}
	return scanJavaScriptPolicyContext(source, context)
}

func TestLoaderInventoryClassifiesEveryPolicyToken(t *testing.T) {
	seen := map[string]bool{}
	policies := []struct {
		name      string
		field     func(loaderInventoryEntry) string
		predicate func(string) bool
	}{
		{"unmodeledJavaScriptLoader", func(row loaderInventoryEntry) string { return row.DeniedTokens }, unmodeledJavaScriptLoader},
		{"unmodeledJavaScriptEnumeration", func(row loaderInventoryEntry) string { return row.EnumerationTokens }, unmodeledJavaScriptEnumeration},
		{"javaScriptGlobalObjectAlias", func(row loaderInventoryEntry) string { return row.GlobalAliases }, javaScriptGlobalObjectAlias},
	}
	for _, row := range loaderInventory {
		if seen[row.ID] || row.ID == "" || !strings.HasPrefix(row.Spec, "https://") || !strings.Contains(row.Spec, "#") || row.API == "" || row.Model == "" {
			t.Errorf("duplicate or undocumented inventory row: %+v", row)
		}
		seen[row.ID] = true
		for _, spec := range row.RelatedSpecs {
			if !strings.HasPrefix(spec, "https://") || !strings.Contains(spec, "#") {
				t.Errorf("undocumented related interface: %s/%s", row.ID, spec)
			}
		}
		switch row.Classification {
		case loaderLiteral:
			found := false
			for _, witness := range loaderInventoryWitnesses[row.ID] {
				found = found || witness.Complete
			}
			if !found {
				t.Errorf("literal model has no positive witness: %s", row.ID)
			}
		case loaderDenied:
			if row.DeniedTokens == "" {
				t.Errorf("fetching API has no denied token: %s", row.ID)
			}
		case loaderNotFetch:
			if row.Fetches {
				t.Errorf("fetching API classified not-fetch: %s", row.ID)
			}
		default:
			t.Errorf("unclassified API: %s", row.ID)
		}
		if len(loaderInventoryWitnesses[row.ID]) == 0 {
			t.Errorf("inventory row lacks independent witness: %s", row.ID)
		}
		if row.Fetches {
			js := false
			for _, witness := range loaderInventoryWitnesses[row.ID] {
				js = js || witness.Kind == KindScript
			}
			if !js {
				t.Errorf("fetching row lacks executable-context witness: %s", row.ID)
			}
		}
	}
	// Independently transcribed from HTML's Fetching scripts definitions.
	// Covers all entry algorithms and the internal graph/descendant fetches.
	for _, anchor := range strings.Fields(`
		fetch-a-classic-script fetch-a-classic-worker-script fetch-a-classic-worker-imported-script
		fetch-a-module-script-tree fetch-a-modulepreload-module-script-graph fetch-an-inline-module-script-graph
		fetch-a-module-worker-script-tree fetch-a-worklet-script-graph fetch-a-worklet/module-worker-script-graph
		fetch-the-descendants-of-and-link-a-module-script fetch-a-single-module-script fetch-a-single-imported-module-script
	`) {
		found := false
		for _, row := range loaderInventory {
			found = found || row.Spec == "https://html.spec.whatwg.org/multipage/webappapis.html#"+anchor
		}
		if !found {
			t.Errorf("HTML script fetch algorithm missing from inventory: %s", anchor)
		}
	}
	for id := range loaderInventoryWitnesses {
		if !seen[id] {
			t.Errorf("stale witness: %s", id)
		}
	}
	// Independently inspect the policy implementation: a future hand-written
	// switch or extra literal capability must trace to an inventory row too.
	file, err := parser.ParseFile(token.NewFileSet(), "references_js_policy.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range policies {
		allowed := inventoryTokens(policy.field)
		for name := range allowed {
			if !policy.predicate(name) {
				t.Errorf("inventory token has no policy: %s/%s", policy.name, name)
			}
		}
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name.Name != policy.name {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				literal, ok := n.(*ast.BasicLit)
				if ok && literal.Kind == token.STRING {
					name, err := strconv.Unquote(literal.Value)
					if err != nil || !allowed[name] {
						t.Errorf("denylist token has no inventory source: %s/%s", policy.name, name)
					}
				}
				return true
			})
		}
	}
	t.Logf("inventory: %d rows; capability=%d enumeration=%d global-alias=%d tokens", len(loaderInventory), len(deniedLoaderTokens), len(enumerationLoaderTokens), len(globalObjectAliases))
}

func TestReferencesGeneratedLoaderInventory(t *testing.T) {
	fetching, executable, native := 0, 0, 0
	for _, row := range loaderInventory {
		if row.Fetches {
			fetching++
		}
		for i, witness := range loaderInventoryWitnesses[row.ID] {
			if witness.Kind == KindScript {
				for _, context := range loaderExecutableContexts {
					t.Run(fmt.Sprintf("%s/%d/%s", row.ID, i, context), func(t *testing.T) {
						source := `fetch("/before.json");` + witness.Source + `import("/after.js");`
						set, err := scanLoaderInventoryContext(t, source, context)
						want := append([]Reference{{"/after.js", KindScript, false}, {"/before.json", KindOther, false}}, witness.References...)
						sort.Slice(want, func(i, j int) bool { return want[i].URL < want[j].URL })
						complete := witness.Complete && !strings.Contains(context, "javascript-url")
						if err != nil || set.Complete != complete || !reflect.DeepEqual(set.Resources, want) {
							t.Fatalf("loader disposition/references: %+v err=%v wantComplete=%v want=%+v source=%s", set, err, complete, want, source)
						}
					})
					executable++
				}
			} else {
				for _, context := range []string{"root", "srcdoc"} {
					t.Run(fmt.Sprintf("%s/%d/native-%s", row.ID, i, context), func(t *testing.T) {
						body, kind := witness.Source, witness.Kind
						if context == "srcdoc" {
							if kind == KindStyle {
								body = "<style>" + body + "</style>"
							}
							body = `<iframe srcdoc="` + html.EscapeString(body) + `"></iframe>`
							kind = KindDocument
						}
						set, err := ScanReferences([]byte(body), kind)
						want := append([]Reference{}, witness.References...)
						if err != nil || set.Complete != witness.Complete || !reflect.DeepEqual(set.Resources, want) {
							t.Fatalf("native loader: %+v err=%v wantComplete=%v want=%+v", set, err, witness.Complete, want)
						}
					})
					native++
				}
			}
		}
	}
	t.Logf("fetching rows=%d executable contexts=%d generated executable cases=%d native cases=%d", fetching, len(loaderExecutableContexts), executable, native)
}

func TestReferencesGeneratedInventoryCapabilityTokens(t *testing.T) {
	count := 0
	for name := range deniedLoaderTokens {
		for _, pattern := range []string{`receiver.NAME("/hidden.js");`, `const alias=receiver.NAME; consume(alias);`, `const {NAME:alias}=receiver; consume(alias);`, `const {"NAME":alias}=receiver; consume(alias);`, `const options={"NAME":"/hidden.js"}; consume(options);`} {
			for _, context := range loaderExecutableContexts {
				t.Run(name+"/"+pattern+"/"+context, func(t *testing.T) {
					source := strings.ReplaceAll(pattern, "NAME", name) + `import("/visible.js");`
					set, err := scanLoaderInventoryContext(t, source, context)
					if err != nil || set.Complete || !reflect.DeepEqual(set.Resources, []Reference{{"/visible.js", KindScript, false}}) {
						t.Fatalf("capability silently certified/lost references: %+v err=%v", set, err)
					}
				})
				count++
			}
		}
	}
	t.Logf("inventory token cases=%d", count)
}

// Specification witnesses are separate from the policy table. Each new row
// requires an example here; each new token gets generated access/alias cases.
var loaderInventoryWitnesses = map[string][]loaderInventoryWitness{
	"classic-script": {
		{Source: "element.src=\"/hidden.js\";", Kind: "js", Complete: false},
		{Source: "<script src=\"/hidden.js\"></script>", Kind: "html", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
	},
	"external-module-graph": {
		{Source: "element.src=\"/hidden.js\";", Kind: "js", Complete: false},
		{Source: "<script type=\"module\" src=\"/hidden.js\"></script>", Kind: "html", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
	},
	"inline-module-graph": {
		{Source: "import(\"/hidden.js\");", Kind: "js", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
		{Source: "<script type=\"module\">import(\"/hidden.js\");</script>", Kind: "html", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
	},
	"module-descendants": {
		{Source: "import(\"/hidden.js\");", Kind: "js", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
		{Source: "<script type=\"module\">import \"/hidden.js\";</script>", Kind: "html", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
	},
	"single-module": {
		{Source: "import(\"/hidden.js\");", Kind: "js", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
		{Source: "<script type=\"module\">import \"/hidden.js\";</script>", Kind: "html", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
	},
	"imported-module": {
		{Source: "import(\"/hidden.js\");", Kind: "js", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
		{Source: "<script type=\"module\">import \"/hidden.js\";</script>", Kind: "html", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
	},
	"modulepreload": {
		{Source: "link.href=\"/hidden.js\";", Kind: "js", Complete: false},
		{Source: "<link rel=\"modulepreload\" href=\"/hidden.js\">", Kind: "html", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
	},
	"classic-worker": {
		{Source: "new Worker(\"/hidden.js\");", Kind: "js", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
	},
	"module-worker": {
		{Source: "new Worker(\"/hidden.js\", {type:\"module\"});", Kind: "js", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
	},
	"shared-classic-worker": {
		{Source: "new SharedWorker(\"/hidden.js\");", Kind: "js", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
	},
	"shared-module-worker": {
		{Source: "new SharedWorker(\"/hidden.js\", {type:\"module\"});", Kind: "js", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
	},
	"worker-importscripts": {
		{Source: "importScripts(\"/hidden.js\");", Kind: "js", Complete: false},
	},
	"worklet": {
		{Source: "CSS.paintWorklet.addModule(\"/hidden.js\");", Kind: "js", Complete: false},
		{Source: "audio.audioWorklet.addModule(\"/hidden.js\");", Kind: "js", Complete: false},
		{Source: "const add=CSS.paintWorklet.addModule; add.call(CSS.paintWorklet,\"/hidden.js\");", Kind: "js", Complete: false},
		{Source: "const {addModule:add}=audio.audioWorklet; add(\"/hidden.js\");", Kind: "js", Complete: false},
	},
	"worklet-graph": {
		{Source: "worklet.addModule(\"/hidden.js\");", Kind: "js", Complete: false},
	},
	"worklet-worker-common-graph": {
		{Source: "new Worker(\"/hidden.js\",{type:\"module\"});", Kind: "js", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
		{Source: "worklet.addModule(\"/hidden.js\");", Kind: "js", Complete: false},
	},
	"import-map": {
		{Source: "<script type=\"importmap\">{\"imports\":{\"pkg\":\"/hidden.js\"}}</script>", Kind: "html", Complete: false},
	},
	"speculation": {
		{Source: "rules.textContent=JSON.stringify({prefetch:[{source:\"list\",urls:[\"/hidden.html\"]}]});", Kind: "js", Complete: false},
		{Source: "<script type=\"speculationrules\">{\"prefetch\":[{\"source\":\"list\",\"urls\":[\"/hidden.html\"]}]}</script>", Kind: "html", Complete: false},
	},
	"image": {
		{Source: "new Image().src=\"/hidden.png\";", Kind: "js", Complete: false},
		{Source: "<img src=\"/hidden.png\">", Kind: "html", Complete: true, References: []Reference{{"/hidden.png", "image", false}}},
	},
	"input-image": {
		{Source: "input.src=\"/hidden.png\";", Kind: "js", Complete: false},
		{Source: "<input type=\"image\" src=\"/hidden.png\">", Kind: "html", Complete: true, References: []Reference{{"/hidden.png", "image", false}}},
	},
	"audio": {
		{Source: "new Audio(\"/hidden.bin\");", Kind: "js", Complete: false},
		{Source: "<audio src=\"/hidden.bin\"></audio>", Kind: "html", Complete: true, References: []Reference{{"/hidden.bin", "other", false}}},
	},
	"video": {
		{Source: "video.src=\"/hidden.bin\";", Kind: "js", Complete: false},
		{Source: "<video src=\"/hidden.bin\"></video>", Kind: "html", Complete: true, References: []Reference{{"/hidden.bin", "other", false}}},
	},
	"media-source": {
		{Source: "source.src=\"/hidden.bin\";", Kind: "js", Complete: false},
		{Source: "<audio><source src=\"/hidden.bin\"></audio>", Kind: "html", Complete: true, References: []Reference{{"/hidden.bin", "other", false}}},
	},
	"track": {
		{Source: "track.src=\"/hidden.vtt\";", Kind: "js", Complete: false},
		{Source: "<video><track src=\"/hidden.vtt\"></video>", Kind: "html", Complete: true, References: []Reference{{"/hidden.vtt", "other", false}}},
	},
	"poster": {
		{Source: "video.poster=\"/hidden.png\";", Kind: "js", Complete: false},
		{Source: "<video poster=\"/hidden.png\"></video>", Kind: "html", Complete: true, References: []Reference{{"/hidden.png", "image", false}}},
	},
	"iframe": {
		{Source: "frame.src=\"/hidden.html\";", Kind: "js", Complete: false},
		{Source: "<iframe src=\"/hidden.html\"></iframe>", Kind: "html", Complete: true, References: []Reference{{"/hidden.html", "html", false}}},
	},
	"stylesheet": {
		{Source: "link.href=\"/hidden.css\";", Kind: "js", Complete: false},
		{Source: "<link rel=\"stylesheet\" href=\"/hidden.css\">", Kind: "html", Complete: true, References: []Reference{{"/hidden.css", "css", false}}},
	},
	"preload": {
		{Source: "link.href=\"/hidden.js\";", Kind: "js", Complete: false},
		{Source: "<link rel=\"preload\" as=\"script\" href=\"/hidden.js\">", Kind: "html", Complete: true, References: []Reference{{"/hidden.js", "js", false}}},
	},
	"prefetch": {
		{Source: "link.href=\"/hidden.bin\";", Kind: "js", Complete: false},
		{Source: "<link rel=\"prefetch\" href=\"/hidden.bin\">", Kind: "html", Complete: true, References: []Reference{{"/hidden.bin", "other", false}}},
	},
	"srcdoc": {
		{Source: "frame.srcdoc=\"<img src=/hidden.png>\";", Kind: "js", Complete: false},
		{Source: "<iframe srcdoc=\"&lt;img src=/hidden.png&gt;\"></iframe>", Kind: "html", Complete: true, References: []Reference{{"/hidden.png", "image", false}}},
	},
	"responsive-image": {
		{Source: "image.srcset=\"/hidden.png 2x\";", Kind: "js", Complete: false},
		{Source: "link.imageSrcset=\"/hidden.png 2x\";", Kind: "js", Complete: false},
		{Source: "<img srcset=\"/hidden.png 2x\">", Kind: "html", Complete: false},
	},
	"embed": {
		{Source: "embed.src=\"/hidden.bin\";", Kind: "js", Complete: false},
		{Source: "<embed src=\"/hidden.bin\">", Kind: "html", Complete: false, References: []Reference{{"/hidden.bin", "other", false}}},
	},
	"object": {
		{Source: "object.data=\"/hidden.bin\";", Kind: "js", Complete: false},
		{Source: "<object data=\"/hidden.bin\"></object>", Kind: "html", Complete: false, References: []Reference{{"/hidden.bin", "other", false}}},
	},
	"icon": {
		{Source: "link.href=\"/hidden.png\";", Kind: "js", Complete: false},
		{Source: "<link rel=\"icon\" href=\"/hidden.png\">", Kind: "html", Complete: false},
	},
	"hyperlink": {
		{Source: "link.href=\"/hidden.html\"; link.click();", Kind: "js", Complete: false},
		{Source: "<a href=\"/hidden.html\">go</a>", Kind: "html", Complete: false},
	},
	"hyperlink-ping": {
		{Source: "link.ping=\"/hidden.json\";", Kind: "js", Complete: false},
		{Source: "<a ping=\"/hidden.json\">go</a>", Kind: "html", Complete: false},
	},
	"web-app-manifest": {
		{Source: "link.href=\"/hidden.webmanifest\";", Kind: "js", Complete: false},
		{Source: "<link rel=\"manifest\" href=\"/hidden.webmanifest\">", Kind: "html", Complete: false},
	},
	"web-app-manifest-images": {
		{Source: "link.href=\"/hidden.webmanifest\";", Kind: "js", Complete: false},
		{Source: "<link rel=\"manifest\" href=\"/hidden.webmanifest\">", Kind: "html", Complete: false},
	},
	"form-navigation": {
		{Source: "form.action=\"/hidden.html\"; form.submit();", Kind: "js", Complete: false},
		{Source: "button.formAction=\"/hidden.html\";", Kind: "js", Complete: false},
		{Source: "<form action=\"/hidden.html\"></form>", Kind: "html", Complete: false},
	},
	"meta-refresh": {
		{Source: "meta.content=\"0;url=/hidden.html\";", Kind: "js", Complete: false},
		{Source: "<meta http-equiv=\"refresh\" content=\"0;url=/hidden.html\">", Kind: "html", Complete: false},
	},
	"navigation": {
		{Source: "location.assign(\"/hidden.html\");", Kind: "js", Complete: false},
		{Source: "window.open(\"/hidden.html\");", Kind: "js", Complete: false},
		{Source: "navigation.navigate(\"/hidden.html\");", Kind: "js", Complete: false},
	},
	"history-traversal": {
		{Source: "history.go(-1);", Kind: "js", Complete: false},
		{Source: "controller.back();", Kind: "js", Complete: false},
	},
	"navigation-traversal": {
		{Source: "controller.traverseTo(entryKey);", Kind: "js", Complete: false},
	},
	"location-components": {
		{Source: "target.pathname=\"/hidden.html\";", Kind: "js", Complete: false},
	},
	"svg-paint-url": {
		{Source: "declaration.fill=\"url(/hidden.svg#paint)\";", Kind: "js", Complete: false},
	},
	"svg-animated-url": {
		{Source: "resource.baseVal=\"/hidden.svg\";", Kind: "js", Complete: false},
	},
	"base-url": {
		{Source: "base.href=\"https://example.test/\";", Kind: "js", Complete: false},
		{Source: "<base href=\"https://example.test/\">", Kind: "html", Complete: false},
	},
	"link-connections": {
		{Source: "link.href=\"https://example.test/\";", Kind: "js", Complete: false},
		{Source: "<link rel=\"preconnect\" href=\"https://example.test/\">", Kind: "html", Complete: false},
	},
	"document-write": {
		{Source: "document.write(\"<img src=/hidden.png>\");", Kind: "js", Complete: false},
		{Source: "document.open();", Kind: "js", Complete: false},
	},
	"dom-create": {
		{Source: "document.createElement(\"img\");", Kind: "js", Complete: false},
	},
	"dom-insert": {
		{Source: "document.head.appendChild(resource);", Kind: "js", Complete: false},
	},
	"markup-parser": {
		{Source: "element.innerHTML=\"<img src=/hidden.png>\";", Kind: "js", Complete: false},
		{Source: "new DOMParser().parseFromString(\"<img src=/hidden.png>\",\"text/html\");", Kind: "js", Complete: false},
	},
	"attribute-mutation": {
		{Source: "element.setAttribute(\"src\",\"/hidden.js\");", Kind: "js", Complete: false},
	},
	"legacy-resource-fields": {
		{Source: "element.background=\"/hidden.png\";", Kind: "js", Complete: false},
	},
	"metadata-url-fields": {
		{Source: "element.cite=\"/description\";", Kind: "js", Complete: false},
	},
	"option-video": {
		{Source: "new Option(\"label\");", Kind: "js", Complete: false},
	},
	"fetch": {
		{Source: "fetch(\"/hidden.bin\");", Kind: "js", Complete: true, References: []Reference{{"/hidden.bin", "other", false}}},
	},
	"fetch-later": {
		{Source: "fetchLater(\"/hidden.bin\");", Kind: "js", Complete: false},
	},
	"request": {
		{Source: "new Request(\"/hidden.bin\");", Kind: "js", Complete: false},
	},
	"xhr": {
		{Source: "const xhr=new XMLHttpRequest(); xhr.open(\"GET\",\"/hidden.bin\"); xhr.send();", Kind: "js", Complete: false},
	},
	"beacon": {
		{Source: "navigator.sendBeacon(\"/hidden.bin\",payload);", Kind: "js", Complete: false},
	},
	"eventsource": {
		{Source: "new EventSource(\"/hidden.bin\");", Kind: "js", Complete: false},
	},
	"websocket": {
		{Source: "new WebSocket(\"wss://example.test/socket\");", Kind: "js", Complete: false},
	},
	"service-worker": {
		{Source: "navigator.serviceWorker.register(\"/hidden.js\");", Kind: "js", Complete: false},
	},
	"service-worker-update": {
		{Source: "registration.update();", Kind: "js", Complete: false},
	},
	"cache-add": {
		{Source: "cache.add(\"/hidden.bin\");", Kind: "js", Complete: false},
	},
	"cache-addall": {
		{Source: "cache.addAll([\"/hidden.bin\"]);", Kind: "js", Complete: false},
	},
	"webtransport": {
		{Source: "new WebTransport(\"https://example.test/transport\");", Kind: "js", Complete: false},
	},
	"background-fetch": {
		{Source: "registration.backgroundFetch.fetch(\"job\",[\"/hidden.bin\"]);", Kind: "js", Complete: false},
		{Source: "manager.fetch(\"job\",[\"/hidden.bin\"]);", Kind: "js", Complete: false},
		{Source: "const {fetch: enqueue}=manager; enqueue(\"job\",[\"/hidden.bin\"]);", Kind: "js", Complete: false},
	},
	"css-import": {
		{Source: "sheet.insertRule(\"@import url(/hidden.css)\");", Kind: "js", Complete: false},
		{Source: "@import \"/hidden.css\";", Kind: "css", Complete: true, References: []Reference{{"/hidden.css", "css", false}}},
	},
	"css-url": {
		{Source: "declaration.setProperty(\"background-image\",\"url(/hidden.png)\");", Kind: "js", Complete: false},
		{Source: ".a{background-image:url(/hidden.png)}", Kind: "css", Complete: true, References: []Reference{{"/hidden.png", "image", false}}},
	},
	"css-property-setters": {
		{Source: "declaration.backgroundImage=\"url(/hidden.png)\";", Kind: "js", Complete: false},
		{Source: "declaration.cursor=\"url(/hidden.png),auto\";", Kind: "js", Complete: false},
	},
	"css-replace": {
		{Source: "sheet.replace(\".a{background:url(/hidden.png)}\");", Kind: "js", Complete: false},
		{Source: "sheet.replaceSync(\".a{background:url(/hidden.png)}\");", Kind: "js", Complete: false},
	},
	"css-typed-om": {
		{Source: "element.attributeStyleMap.set(\"background-image\",\"url(/hidden.png)\");", Kind: "js", Complete: false},
	},
	"fontface": {
		{Source: "new FontFace(\"Fixture\",\"url(/hidden.woff2)\").load();", Kind: "js", Complete: false},
	},
	"fontset": {
		{Source: "document.fonts.load(\"16px Fixture\");", Kind: "js", Complete: false},
	},
	"notification": {
		{Source: "new Notification(\"title\",{icon:\"/hidden.png\",badge:\"/badge.png\",image:\"/image.png\",actions:[{action:\"go\",title:\"go\",icon:\"/action.png\"}]});", Kind: "js", Complete: false},
	},
	"push-notification": {
		{Source: "registration.showNotification(\"title\",{icon:\"/hidden.png\"});", Kind: "js", Complete: false},
	},
	"push-subscribe": {
		{Source: "registration.pushManager.subscribe({userVisibleOnly:true});", Kind: "js", Complete: false},
		{Source: "manager.subscribe({userVisibleOnly:true});", Kind: "js", Complete: false},
	},
	"payment-method": {
		{Source: "new PaymentRequest([{supportedMethods:\"https://example.test/pay\"}],details).show();", Kind: "js", Complete: false},
	},
	"payment-app": {
		{Source: "request.show();", Kind: "js", Complete: false},
	},
	"url": {
		{Source: "new URL(\"/hidden.png\",import.meta.url);", Kind: "js", Complete: true, References: []Reference{{"/hidden.png", "image", false}}},
	},
	"timers": {
		{Source: "setTimeout(()=>fetch(\"/hidden.bin\"),10);", Kind: "js", Complete: true, References: []Reference{{"/hidden.bin", "other", false}}},
		{Source: "setInterval(()=>fetch(\"/hidden.bin\"),10);", Kind: "js", Complete: true, References: []Reference{{"/hidden.bin", "other", false}}},
	},
	"code-construction": {
		{Source: "new Function(\"fetch('/hidden.bin')\")();", Kind: "js", Complete: false},
	},
	"reflection": {
		{Source: "Reflect.get(receiver,\"fetch\");", Kind: "js", Complete: false},
	},
	"enumeration": {
		{Source: "Object.values(receiver);", Kind: "js", Complete: false},
	},
	"global-aliases": {
		{Source: "consume(globalThis);", Kind: "js", Complete: false},
	},
	"custom-elements": {
		{Source: "customElements.define(\"fixture-node\",Fixture);", Kind: "js", Complete: false},
	},
}
