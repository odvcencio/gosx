package wire

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func executableAttributeDocument(kind, code string) string {
	switch kind {
	case "handler":
		return `<button onclick="` + html.EscapeString(code) + `"></button>`
	case "svg-handler":
		return `<svg onload="` + html.EscapeString(code) + `"></svg>`
	case "javascript-url":
		return `<a href="javascript:` + html.EscapeString(code) + `">run</a>`
	case "svg-javascript-url":
		return `<svg><a href="javascript:` + html.EscapeString(code) + `"></a></svg>`
	default:
		return `<script>` + code + `</script>`
	}
}

func TestReferencesExecutableAttributePlacements(t *testing.T) {
	for _, kind := range []string{"handler", "svg-handler", "javascript-url", "svg-javascript-url", "script"} {
		for _, code := range []string{`fetch("/literal.json")`, `fetch(window.fixtureURL)`, `document.createElement("script"); import("/visible.js")`} {
			for _, placement := range []string{"root", "srcdoc", "fetched-child"} {
				t.Run(kind+"/"+code+"/"+placement, func(t *testing.T) {
					body := executableAttributeDocument(kind, code)
					switch placement {
					case "srcdoc":
						body = `<iframe srcdoc="` + html.EscapeString(body) + `"></iframe>`
					case "fetched-child":
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							w.Header().Set("Content-Type", "text/html")
							if r.URL.Path == "/" {
								fmt.Fprint(w, `<iframe src="/child"></iframe>`)
								return
							}
							fmt.Fprint(w, body)
						}))
						defer server.Close()
						root, err := server.Client().Get(server.URL + "/")
						if err != nil {
							t.Fatal(err)
						}
						raw, err := io.ReadAll(root.Body)
						root.Body.Close()
						if err != nil {
							t.Fatal(err)
						}
						edges, err := ScanReferences(raw, KindDocument)
						if err != nil || !edges.Complete || !reflect.DeepEqual(referenceValues(edges.Resources), []Reference{{URL: "/child", Kind: KindDocument, Potential: false}}) {
							t.Fatalf("fetched child edge: %+v %v", edges, err)
						}
						child, err := server.Client().Get(server.URL + edges.Resources[0].URL)
						if err != nil {
							t.Fatal(err)
						}
						raw, err = io.ReadAll(child.Body)
						child.Body.Close()
						if err != nil {
							t.Fatal(err)
						}
						body = string(raw)
					}
					set, err := ScanReferences([]byte(body), KindDocument)
					if err != nil {
						t.Fatal(err)
					}
					target := "/literal.json"
					if strings.Contains(code, "createElement") {
						target = "/visible.js"
					}
					if strings.Contains(code, "fixtureURL") {
						if set.Complete {
							t.Fatalf("computed handler/URL fetch certified complete: %+v", set)
						}
					} else {
						wantKind := KindOther
						if target == "/visible.js" {
							wantKind = KindScript
						}
						if !reflect.DeepEqual(referenceValues(set.Resources), []Reference{{URL: target, Kind: wantKind, Potential: false}}) {
							t.Fatalf("executable reference lost: %+v", set)
						}
						if strings.Contains(code, "createElement") && set.Complete {
							t.Fatalf("denylisted handler/URL certified complete: %+v", set)
						}
						if !strings.HasPrefix(kind, "svg-") && kind != "javascript-url" && target == "/literal.json" && !set.Complete {
							t.Fatalf("modelled literal fetch unnecessarily incomplete: %+v", set)
						}
					}
				})
			}
		}
	}
}

func TestReferencesExecutableAttributeInertAndSandboxed(t *testing.T) {
	for _, kind := range []string{"handler", "javascript-url", "script"} {
		active := executableAttributeDocument(kind, `fetch(window.fixtureURL)`)
		for _, body := range []string{
			`<template>` + active + `</template>`,
			`<iframe sandbox srcdoc="` + html.EscapeString(active) + `"></iframe>`,
			`<iframe sandbox srcdoc="` + html.EscapeString(`<iframe sandbox="allow-scripts" srcdoc="`+html.EscapeString(active)+`"></iframe>`) + `"></iframe>`,
		} {
			drops := []referenceDropReason{}
			set, err := scanReferences([]byte(body), KindDocument, func(reason referenceDropReason) { drops = append(drops, reason) })
			if err != nil || !set.Complete || len(set.Resources) != 0 {
				t.Errorf("inert executable source scanned: %s set=%+v drops=%v err=%v", body, set, drops, err)
			}
			wantReason := dropSandboxedExecutable
			if strings.HasPrefix(body, "<template>") {
				wantReason = dropTemplateContent
			}
			found := false
			for _, reason := range drops {
				found = found || reason == wantReason
			}
			if !found {
				t.Errorf("inert source omitted without expected typed reason: drops=%v want=%v", drops, wantReason)
			}
		}
	}
}

func TestReferencesExecutableAttributeControls(t *testing.T) {
	for _, code := range []string{`return fetch("/literal.json")`, `return false; fetch("/literal.json")`, `if(event) { fetch("/literal.json"); }`} {
		set, err := ScanReferences([]byte(executableAttributeDocument("handler", code)), KindDocument)
		if err != nil || !set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), []Reference{{URL: "/literal.json", Kind: KindOther, Potential: false}}) {
			t.Errorf("handler FunctionBody lost closure: %s %+v %v", code, set, err)
		}
	}
	for _, target := range []string{`javascript:fetch("/literal.json")`, ` JaVaScRiPt:fetch("/literal.json")`, "java\tscr\nipt:fetch(\"/literal.json\")", `javascript:%66etch%28%22%2Fliteral.json%22%29`} {
		body := `<a href="` + html.EscapeString(target) + `"></a>`
		set, err := ScanReferences([]byte(body), KindDocument)
		if err != nil || set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), []Reference{{URL: "/literal.json", Kind: KindOther, Potential: false}}) {
			t.Errorf("executable URL normalization or completion uncertainty lost: %q %+v %v", target, set, err)
		}
	}
	for _, suffix := range []string{`javascript:fetch(window.fixtureURL)`, `javascript:'<img src="/hidden.png">'`} {
		body := `<meta http-equiv="refresh" content="` + html.EscapeString("0; URL="+suffix) + `">`
		set, err := ScanReferences([]byte(body), KindDocument)
		if err != nil || set.Complete {
			t.Errorf("refresh executable URL claimed complete: %+v %v", set, err)
		}
	}
	body := `<meta http-equiv="refresh" content="0; URL=javascript:fetch('/literal.json')">`
	set, err := ScanReferences([]byte(body), KindDocument)
	if err != nil || set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), []Reference{{URL: "/literal.json", Kind: KindOther, Potential: false}}) {
		t.Errorf("refresh URL dropped fetch: %+v %v", set, err)
	}
	// A sandbox affects the embedded document, not the parent's iframe handler.
	for _, attributes := range []string{
		`sandbox onload="fetch('/outer.json')" srcdoc="&lt;button onclick=&quot;fetch(window.fixtureURL)&quot;&gt;&lt;/button&gt;"`,
		`srcdoc="&lt;button onclick=&quot;fetch(window.fixtureURL)&quot;&gt;&lt;/button&gt;" onload="fetch('/outer.json')" sandbox`,
	} {
		set, err := ScanReferences([]byte("<iframe "+attributes+"></iframe>"), KindDocument)
		if err != nil || !set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), []Reference{{URL: "/outer.json", Kind: KindOther, Potential: false}}) {
			t.Errorf("embedded sandbox leaked to outer handler: %+v %v", set, err)
		}
	}
	for _, sandbox := range []string{"allow-scripts", "ALLOW-SCRIPTS", "allow-same-origin\tallow-scripts"} {
		body := `<iframe sandbox="` + sandbox + `" srcdoc="` + html.EscapeString(executableAttributeDocument("handler", `fetch("/literal.json")`)) + `"></iframe>`
		set, err := ScanReferences([]byte(body), KindDocument)
		if err != nil || !set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), []Reference{{URL: "/literal.json", Kind: KindOther, Potential: false}}) {
			t.Errorf("allow-scripts handler dropped: %+v %v", set, err)
		}
	}
	for _, attributes := range []string{`src="javascript:fetch(window.fixtureURL)" srcdoc=""`, `srcdoc="" src="javascript:fetch(window.fixtureURL)"`} {
		set, err := ScanReferences([]byte("<iframe "+attributes+"></iframe>"), KindDocument)
		if err != nil || !set.Complete || len(set.Resources) != 0 {
			t.Errorf("overridden frame source executed: %+v %v", set, err)
		}
	}
	body = `<iframe sandbox srcdoc="` + html.EscapeString(`<script src="/ignored.js">fetch(window.fixtureURL)</script><img src="/live.png"><button onclick="fetch(window.fixtureURL)"></button>`) + `"></iframe>`
	set, err = ScanReferences([]byte(body), KindDocument)
	if err != nil || !set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), []Reference{{URL: "/live.png", Kind: KindImage, Potential: false}}) {
		t.Errorf("sandbox lost native resource or executed script: %+v %v", set, err)
	}
	body = `<script type="application/json" src="javascript:fetch(window.fixtureURL)"></script>`
	set, err = ScanReferences([]byte(body), KindDocument)
	if err != nil || !set.Complete || len(set.Resources) != 0 {
		t.Errorf("inert data-script source executed: %+v %v", set, err)
	}
}

func TestReferencesSrcdocBounds(t *testing.T) {
	body := executableAttributeDocument("handler", `fetch("/literal.json")`)
	for depth := 1; depth <= 33; depth++ {
		body = `<iframe srcdoc="` + html.EscapeString(body) + `"></iframe>`
		set, err := ScanReferences([]byte(body), KindDocument)
		if depth <= 32 {
			if err != nil || !set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), []Reference{{URL: "/literal.json", Kind: KindOther, Potential: false}}) {
				t.Fatalf("permitted srcdoc depth %d: %+v %v", depth, set, err)
			}
		} else if err == nil || set.Complete {
			t.Fatalf("srcdoc beyond bound certified: %+v %v", set, err)
		}
	}
}

func TestReferencesTemplateOwnedExecutableAttributes(t *testing.T) {
	// Template contents are inert. Its own attributes must not inherit that
	// exemption and silently hide a callable handler or unknown URL selector.
	for _, attribute := range []string{`onclick="fetch('/owned.json')"`, `href="javascript:fetch('/owned.json')"`} {
		set, err := ScanReferences([]byte(`<template `+attribute+`><button onclick="fetch('/inert.json')"></button></template>`), KindDocument)
		if err != nil || set.Complete || !reflect.DeepEqual(referenceValues(set.Resources), []Reference{{URL: "/owned.json", Kind: KindOther, Potential: false}}) {
			t.Errorf("template owner inherited content exemption: %+v %v", set, err)
		}
	}
}
