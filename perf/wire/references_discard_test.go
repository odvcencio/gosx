package wire

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestReferencesManifestIDDoesNotSuppressScript(t *testing.T) {
	for _, typ := range []string{"", "module", "text/javascript"} {
		for _, id := range []string{"", ` id="gosx-manifest"`} {
			body := `<script` + id + ` type="` + typ + `" src="/active.js">{"version":"0.1.0"}</script>`
			set, err := ScanReferences([]byte(body), KindDocument)
			if err != nil || !set.Complete || !reflect.DeepEqual(set.Resources, []Reference{{"/active.js", KindScript, false}}) {
				t.Errorf("executable src suppressed: type=%q id=%q set=%+v err=%v", typ, id, set, err)
			}
		}
	}
	// An ID does not turn executable inline source into manifest data either.
	set, err := ScanReferences([]byte(`<script id="gosx-manifest">fetch("/active.js")</script>`), KindDocument)
	if err != nil || !set.Complete || !reflect.DeepEqual(set.Resources, []Reference{{"/active.js", KindScript, false}}) {
		t.Fatalf("inline executable manifest ID: %+v %v", set, err)
	}
	// Data scripts with src do not execute or supply an inline manifest.
	set, err = ScanReferences([]byte(`<script type="application/json" id="gosx-manifest" src="/ignored.js">{"version":"0.1.0","islands":[{"programRef":"/ignored.bin"}],"runtime":{"path":"/ignored.wasm"}}</script>`), KindDocument)
	if err != nil || !set.Complete || len(set.Resources) != 0 {
		t.Fatalf("external data script selected a manifest: %+v %v", set, err)
	}
}

// The independent HTML-spec table also supplies obsolete and foreign URL
// attributes. Cross it with every element, including invalid placements (which
// must fail closed), rather than limiting cases to the implemented allowlist.
// DOM construction keeps parser repair of invalid markup from erasing a test's
// subject. Public ScanReferences controls exercise parsing in the other corpora.
func TestReferencesGeneratedDiscardMatrix(t *testing.T) {
	elements := strings.Fields(htmlAttributeIndexElements + " applet bgsound frame param svg:image svg:script svg:use resource-fixture")
	attributes := []string{}
	seen := map[string]bool{}
	for _, row := range htmlURLAttributeCases {
		if !seen[row.attribute] {
			attributes = append(attributes, row.attribute)
			seen[row.attribute] = true
		}
	}
	attributes = append(attributes, "data-gosx-future-url", "future-url", "onclick", "is")
	contexts := []string{"live", "manifest-id", "module", "data-script", "unknown-type", "empty", "fragment", "opaque-data", "template", "shadow-template", "sandboxed-frame"}
	count, references, incomplete, inert := 0, 0, 0, 0
	for _, element := range elements {
		for _, attribute := range attributes {
			for _, context := range contexts {
				count++
				value := "/candidate.png"
				if context == "empty" {
					value = ""
				} else if context == "fragment" {
					value = "#candidate"
				} else if context == "opaque-data" {
					value = "data:image/png;base64,AAAA"
				}
				if attribute == "style" && value != "" {
					value = `background:url("` + value + `")`
				}
				n := &html.Node{Type: html.ElementNode, Data: element, Attr: []html.Attribute{{Key: attribute, Val: value}}}
				if strings.HasPrefix(element, "svg:") {
					n.Namespace, n.Data = "svg", strings.TrimPrefix(element, "svg:")
				}
				if element == "input" {
					n.Attr = append(n.Attr, html.Attribute{Key: "type", Val: "image"})
				} else if element == "link" {
					n.Attr = append(n.Attr, html.Attribute{Key: "rel", Val: "stylesheet"})
				} else if element == "meta" {
					n.Attr = append(n.Attr, html.Attribute{Key: "http-equiv", Val: "refresh"})
				}
				if context == "manifest-id" {
					n.Attr = append(n.Attr, html.Attribute{Key: "id", Val: "gosx-manifest"})
				}
				if element == "script" {
					typ := map[string]string{"module": "module", "data-script": "application/json", "unknown-type": "future-loader"}[context]
					n.Attr = append(n.Attr, html.Attribute{Key: "type", Val: typ})
				}
				root := n
				if context == "template" || context == "shadow-template" {
					root = &html.Node{Type: html.ElementNode, Data: "template"}
					root.AppendChild(n)
					if context == "shadow-template" {
						root.Attr = []html.Attribute{{Key: "shadowrootmode", Val: "open"}}
					}
				} else if context == "sandboxed-frame" {
					root = &html.Node{Type: html.ElementNode, Data: "iframe", Attr: []html.Attribute{{Key: "sandbox"}, {Key: "srcdoc", Val: htmlAttributeDocument(element, attribute+`="`+value+`"`)}}}
				}
				drops := []referenceDropReason{}
				set := ReferenceSet{Resources: []Reference{}, Complete: true, onDrop: func(reason referenceDropReason) { drops = append(drops, reason) }}
				err := scanDocumentTree(root, &set)
				wantReason := dropUnresolved
				switch {
				case context == "template":
					wantReason = dropTemplateContent
				case context == "sandboxed-frame" || context == "shadow-template":
				case element == "script" && attribute == "src" && context == "data-script":
					wantReason = dropInertDataScript
				case attribute == "style" && element == "template":
					wantReason = dropTemplateContent
				case attribute == "style" && context == "empty":
					wantReason = dropEmptySyntax
				case attribute == "style" && context == "fragment":
					wantReason = dropCSSFragment
				case context == "opaque-data" && (attribute == "style" || attribute == "src" && (element == "img" || element == "input") || attribute == "poster" && element == "video"):
					wantReason = dropOpaqueData
				case context == "empty" && (attribute == "srcset" && (element == "img" || element == "source") || attribute == "imagesrcset" && element == "link"):
					wantReason = dropInertHTML
				case attribute == "value" && strings.Contains(" button data input li meter option progress ", " "+element+" "):
					wantReason = dropInertHTML
				}
				if context != "template" && context != "shadow-template" && context != "sandboxed-frame" {
					// Isolate the subject attribute as well: an inert sibling such as
					// type/id must not provide a witness for a silently dropped value.
					isolatedDrops := []referenceDropReason{}
					isolated := ReferenceSet{Complete: true, onDrop: func(reason referenceDropReason) { isolatedDrops = append(isolatedDrops, reason) }}
					isolatedErr := scanHTMLReferenceAttribute(n, n.Attr[0], &isolated)
					assertReferenceDisposition(t, element+"/"+attribute+"/"+context+"/attribute", isolated, isolatedErr, isolatedDrops, wantReason, "/candidate.png")
				}
				// These are disjoint outcomes: prefer the subject's reference,
				// otherwise require incompleteness, otherwise an expected safe drop.
				found := false
				for _, ref := range set.Resources {
					found = found || ref.URL == "/candidate.png"
				}
				if found {
					references++
				} else if !set.Complete {
					incomplete++
				} else {
					inert++
					matched := false
					for _, reason := range drops {
						matched = matched || reason == wantReason && reason != dropUnresolved
					}
					if !matched || err != nil {
						t.Errorf("silent/unjustified drop: %s/%s/%s set=%+v drops=%v want=%v err=%v", element, attribute, context, set.Resources, drops, wantReason, err)
					}
				}
			}
		}
	}
	t.Logf("elements=%d attributes=%d contexts=%d cases=%d reference=%d incomplete=%d justified=%d", len(elements), len(attributes), len(contexts), count, references, incomplete, inert)
	if count != 46585 {
		t.Fatalf("discard matrix changed: %d cases; audit any inventory change", count)
	}
}

func assertReferenceDisposition(t *testing.T, name string, set ReferenceSet, err error, drops []referenceDropReason, wantReason referenceDropReason, target string) {
	t.Helper()
	for _, ref := range set.Resources {
		if ref.URL == target {
			return
		}
	}
	if !set.Complete {
		return
	}
	for _, reason := range drops {
		if reason == wantReason && reason != dropUnresolved && err == nil {
			return
		}
	}
	t.Errorf("silent/unjustified drop: %s resources=%+v drops=%v want=%v err=%v", name, set.Resources, drops, wantReason, err)
}

func TestReferencesGeneratedManifestDispositions(t *testing.T) {
	contexts := []struct {
		name, attributes string
		reason           referenceDropReason
	}{
		{"selected", `type="application/json" id="gosx-manifest"`, dropUnresolved},
		{"selected-other-data-type", `type="text/plain" id="gosx-manifest"`, dropUnresolved},
		{"no-id", `type="application/json"`, dropInertDataScript},
		{"other-id", `type="application/json" id="other"`, dropInertDataScript},
		{"data-src", `type="application/json" id="gosx-manifest" src="/external.js"`, dropInertDataScript},
		{"executable-src", `id="gosx-manifest" src="/external.js"`, dropExternalScriptBody},
		{"executable-module-src", `type="module" id="gosx-manifest" src="/external.js"`, dropExternalScriptBody},
		// Plain JSON strings are not JavaScript fetch expressions. The ID does
		// not select them as a manifest; syntax errors may still fail closed.
		{"executable-inline", `id="gosx-manifest"`, dropNonLoadingSyntax},
		{"template", `type="application/json" id="gosx-manifest"`, dropTemplateContent},
		{"sandboxed-frame", `type="application/json" id="gosx-manifest"`, dropUnresolved},
	}
	count := 0
	for _, slot := range manifestFetchCases() {
		for _, target := range []string{"", " ", "#candidate", " #candidate ", "/candidate.png", "data:application/javascript,fetch('/hidden')", "blob:opaque"} {
			for _, context := range contexts {
				count++
				raw := `{"version":"0.1.0",` + fmt.Sprintf(slot.fields, fmt.Sprintf("%q", target)) + `}`
				body := `<script ` + context.attributes + `>` + raw + `</script>`
				if context.name == "template" {
					body = "<template>" + body + "</template>"
				} else if context.name == "sandboxed-frame" {
					body = `<iframe sandbox srcdoc="` + html.EscapeString(body) + `"></iframe>`
				}
				drops := []referenceDropReason{}
				set, err := scanReferences([]byte(body), KindDocument, func(reason referenceDropReason) { drops = append(drops, reason) })
				assertReferenceDisposition(t, slot.name+"/"+target+"/"+context.name, set, err, drops, context.reason, strings.TrimSpace(target))
			}
		}
	}
	// Inactive paths use the same closed policy; fragments here are genuinely
	// unselected inventory, unlike the selected fetches exercised above.
	for _, fields := range []string{
		`"runtime":{"path":%s}`,
		`"bundles":{"dormant":{"path":%s}}`,
		`"islands":[{"static":true,"programRef":%s}],"runtime":{"path":"/known.wasm"}`,
	} {
		for _, target := range []string{"", "#candidate", "/candidate.png"} {
			count++
			body := `<script type="application/json" id="gosx-manifest">{"version":"0.1.0",` + fmt.Sprintf(fields, fmt.Sprintf("%q", target)) + `}</script>`
			drops := []referenceDropReason{}
			set, err := scanReferences([]byte(body), KindDocument, func(reason referenceDropReason) { drops = append(drops, reason) })
			assertReferenceDisposition(t, "dormant/"+fields+"/"+target, set, err, drops, dropDormantManifest, target)
		}
	}
	t.Logf("manifest slots=%d contexts=%d cases=%d", len(manifestFetchCases()), len(contexts), count)
	if count != 429 {
		t.Fatalf("manifest matrix changed: %d cases; audit any inventory change", count)
	}
}

func TestReferencesDropPolicyFailsClosed(t *testing.T) {
	for _, reason := range []referenceDropReason{dropUnresolved, 255, dropEmptySyntax + 1} {
		set := ReferenceSet{Complete: true}
		set.drop(reason)
		if set.Complete {
			t.Fatalf("unknown/unresolved reason preserved coverage: %v", reason)
		}
	}
	// Future manifest fields are unresolved while known selected paths survive.
	set, err := ScanReferences([]byte(`<script type="application/json" id="gosx-manifest">{"version":"0.1.0","futurePath":"/unknown.js","islands":[{"programRef":"/known.bin"}],"runtime":{"path":"/known.wasm"}}</script>`), KindDocument)
	if err != nil || set.Complete || len(set.Resources) != 2 {
		t.Fatalf("unknown manifest fields lost uncertainty or exact references: %+v %v", set, err)
	}
}

type manifestFetchCase struct {
	name, fields, kind string
	potential          bool
}

func manifestFetchCases() []manifestFetchCase {
	return []manifestFetchCase{
		{"runtime", `"islands":[{"static":true}],"runtime":{"path":%s}`, KindWASM, false},
		{"conditional-runtime", `"hubs":[{}],"runtime":{"path":%s}`, KindWASM, true},
		{"island-program", `"islands":[{"programRef":%s}],"runtime":{"path":"/known.wasm"}`, KindProgram, false},
		{"compute-program", `"computeIslands":[{"programRef":%s}],"runtime":{"path":"/known.wasm"}`, KindProgram, false},
		{"engine-program", `"engines":[{"runtime":"go-wasm","programRef":%s}]`, KindWASM, false},
		{"selected-bundle", `"islands":[{"bundleId":"active"}],"bundles":{"active":{"path":%s}},"runtime":{"path":"/known.wasm"}`, KindWASM, false},
	}
}

func TestReferencesSelectedManifestFetchTargets(t *testing.T) {
	for _, tc := range manifestFetchCases() {
		for _, target := range []string{"", " ", "#current", " #current ", "/selected.wasm"} {
			t.Run(tc.name+"/"+target, func(t *testing.T) {
				body := `<script type="application/json" id="gosx-manifest">{"version":"0.1.0",` + fmt.Sprintf(tc.fields, fmt.Sprintf("%q", target)) + `}</script>`
				set, err := ScanReferences([]byte(body), KindDocument)
				if err != nil {
					t.Fatal(err)
				}
				if strings.TrimSpace(target) == "" || strings.HasPrefix(strings.TrimSpace(target), "#") {
					if set.Complete {
						t.Fatalf("selected current-document fetch disappeared: %+v", set)
					}
					return
				}
				want := Reference{target, tc.kind, tc.potential}
				found := false
				for _, ref := range set.Resources {
					found = found || ref == want
				}
				if !found || set.Complete == tc.potential {
					t.Fatalf("selected target lost kind or potential status: %+v want %+v", set, want)
				}
			})
		}
	}
}
