package wire

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"

	"golang.org/x/net/html"
)

// The JS loader test builds these same document trees in the bootstrap DOM
// shim. Go renders and reparses them, rather than bypassing HTML parsing.
type manifestCorpusNode struct {
	Tag        string                `json:"tag"`
	Key        string                `json:"key"`
	Text       *string               `json:"text"`
	Attributes map[string]string     `json:"attributes"`
	Children   []manifestCorpusNode  `json:"children"`
	Srcdoc     *[]manifestCorpusNode `json:"srcdoc"`
}

type manifestCorpusSelection struct {
	Candidate string          `json:"candidate"`
	Manifest  json.RawMessage `json:"manifest"`
}

type manifestCorpusCase struct {
	Name          string                    `json:"name"`
	Document      []manifestCorpusNode      `json:"document"`
	Expected      manifestCorpusSelection   `json:"expected"`
	References    []Reference               `json:"references"`
	ChildExpected []manifestCorpusSelection `json:"childExpected"`
}

func renderManifestCorpus(nodes []manifestCorpusNode) string {
	var renderNode func(manifestCorpusNode) *html.Node
	renderNode = func(source manifestCorpusNode) *html.Node {
		if source.Text != nil {
			return &html.Node{Type: html.TextNode, Data: *source.Text}
		}
		n := &html.Node{Type: html.ElementNode, Data: source.Tag}
		keys := make([]string, 0, len(source.Attributes))
		for key := range source.Attributes {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			n.Attr = append(n.Attr, html.Attribute{Key: key, Val: source.Attributes[key]})
		}
		if source.Key != "" {
			n.Attr = append(n.Attr, html.Attribute{Key: "data-corpus-key", Val: source.Key})
		}
		if source.Srcdoc != nil {
			n.Attr = append(n.Attr, html.Attribute{Key: "srcdoc", Val: renderManifestCorpus(*source.Srcdoc)})
		}
		for _, child := range source.Children {
			n.AppendChild(renderNode(child))
		}
		return n
	}
	var body bytes.Buffer
	for _, node := range nodes {
		if err := html.Render(&body, renderNode(node)); err != nil {
			panic(err)
		}
	}
	return body.String()
}

func TestReferencesManifestSelectionCorpus(t *testing.T) {
	raw, err := os.ReadFile("testdata/manifest-selection.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []manifestCorpusCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			body := renderManifestCorpus(fixture.Document)
			root, err := html.Parse(bytes.NewBufferString(body))
			if err != nil {
				t.Fatal(err)
			}
			assertManifestCorpusSelection(t, root, fixture.Expected)
			child := 0
			var visit func(*html.Node)
			visit = func(n *html.Node) {
				if n.Type == html.ElementNode && n.Data == "template" && n.Namespace == "" {
					return
				}
				if n.Type == html.ElementNode && n.Data == "iframe" && hasHTMLReferenceAttribute(n, "srcdoc") {
					if child >= len(fixture.ChildExpected) {
						t.Fatal("unexpected srcdoc document")
					}
					root, err := html.Parse(bytes.NewBufferString(attr(n, "srcdoc")))
					if err != nil {
						t.Fatal(err)
					}
					assertManifestCorpusSelection(t, root, fixture.ChildExpected[child])
					child++
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					visit(c)
				}
			}
			visit(root)
			if child != len(fixture.ChildExpected) {
				t.Fatalf("srcdoc documents=%d want=%d", child, len(fixture.ChildExpected))
			}
			set, err := ScanReferences([]byte(body), KindDocument)
			if !reflect.DeepEqual(set.Resources, fixture.References) {
				t.Fatalf("loader/scanner disagreement: got=%+v want=%+v complete=%v err=%v", set.Resources, fixture.References, set.Complete, err)
			}
		})
	}
	t.Logf("shared loader/scanner documents=%d", len(cases))
	if len(cases) != 89 {
		t.Fatalf("shared corpus changed: %d; audit selection coverage", len(cases))
	}
}

func assertManifestCorpusSelection(t *testing.T, root *html.Node, expected manifestCorpusSelection) {
	t.Helper()
	element, err := documentManifestElement(root)
	if err != nil {
		t.Fatal(err)
	}
	key := ""
	var value any
	if element != nil {
		key = attr(element, "data-corpus-key")
		raw, err := manifestElementTextContent(element)
		if err != nil {
			t.Fatal(err)
		}
		// JSON.parse failures yield null from the real loader.
		_ = json.Unmarshal([]byte(raw), &value)
	}
	var want any
	if err := json.Unmarshal(expected.Manifest, &want); err != nil {
		t.Fatal(err)
	}
	if key != expected.Candidate || !reflect.DeepEqual(value, want) {
		t.Fatalf("manifest selection: candidate=%q value=%v want=%+v", key, value, expected)
	}
}

func TestReferencesManifestDataScriptWithSrc(t *testing.T) {
	body := `<script type="application/json" id="gosx-manifest" src="/ignored.js">{"version":"0.1.0","islands":[{"programRef":"/active.bin"}],"runtime":{"path":"/active.wasm"}}</script>`
	set, err := ScanReferences([]byte(body), KindDocument)
	want := []Reference{{"/active.bin", KindProgram, false}, {"/active.wasm", KindWASM, false}}
	if err != nil || !set.Complete || !reflect.DeepEqual(set.Resources, want) {
		t.Fatalf("manifest textContent dependencies lost: %+v err=%v", set, err)
	}
}

func TestReferencesInvalidFirstManifestDoesNotFallThrough(t *testing.T) {
	for _, first := range []string{`<div id="gosx-manifest">{invalid}</div>`, `<script type="application/json" id="gosx-manifest"></script>`, `<template id="gosx-manifest"><div>{"version":"0.1.0"}</div></template>`} {
		body := first + `<script type="application/json" id="gosx-manifest">{"version":"0.1.0","islands":[{"programRef":"/later.bin"}],"runtime":{"path":"/later.wasm"}}</script>`
		drops := []referenceDropReason{}
		set, err := scanReferences([]byte(body), KindDocument, func(r referenceDropReason) { drops = append(drops, r) })
		if err != nil || !set.Complete || len(set.Resources) != 0 {
			t.Fatalf("invalid first candidate fell through: %+v err=%v", set, err)
		}
		found := false
		for _, reason := range drops {
			found = found || reason == dropInvalidManifest
		}
		if !found {
			t.Fatalf("parse failure silently dropped: %v", drops)
		}
	}
}
