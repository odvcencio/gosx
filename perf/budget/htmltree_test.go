package budget

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"mime"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func TestInlineForeignTemplateUsesHTMLNamespace(t *testing.T) {
	// WHATWG HTML, parsing foreign content: only a template in the HTML
	// namespace has inert contents; SVG/MathML integration points change the
	// namespace of their descendants, not that of the foreign template.
	for _, item := range []struct {
		name, body string
		scripts    int64
	}{
		{"ordinary", `<script nonce="fixture">app()</script>`, 1},
		{"html-template", `<template><script nonce="fixture">app()</script></template>`, 0},
		{"svg-template-script", `<svg><template><script nonce="fixture">app()</script></template></svg>`, 1},
		{"svg-template", `<svg><template><foreignObject><script nonce="fixture">app()</script></foreignObject></template></svg>`, 1},
		{"math-template", `<math><template><annotation-xml encoding="text/html"><script nonce="fixture">app()</script></annotation-xml></template></math>`, 1},
	} {
		t.Run(item.name, func(t *testing.T) {
			body := []byte(item.body)
			result, err := measureHTML(body, HTMLMeasureOptions{}, testBodyNormalizer)
			if err != nil {
				t.Fatal(err)
			}
			if result.ExecutableScripts != item.scripts {
				t.Errorf("script count: got %d want %d", result.ExecutableScripts, item.scripts)
			}
			wantMax := int64(0)
			if item.scripts != 0 {
				wantMax = int64(len("app()"))
			}
			if result.InlineAppScriptMax != wantMax {
				t.Errorf("inline maximum: got %d want %d", result.InlineAppScriptMax, wantMax)
			}
			if !bytes.Equal(result.full, body) {
				t.Error("raw document changed")
			}
			if err := VerifyHTMLNonces(body, "script-src 'none'"); (err != nil) != (item.scripts != 0) {
				t.Errorf("CSP verdict: %v", err)
			}
			if err := VerifyHTMLNonces(body, "script-src 'nonce-fixture'"); err != nil {
				t.Errorf("matching nonce rejected: %v", err)
			}
			opts := testHTTPOptions(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Content-Security-Policy", "script-src 'none'")
				w.Write(body)
			}, body)
			opts.Kind = "html"
			if _, err := measureHTTP(context.Background(), opts, testBodyNormalizer); (err != nil) != (item.scripts != 0) {
				t.Errorf("HTTP CSP verdict: %v", err)
			}
		})
	}
}

// The independent oracle parses the original bytes, never the classifier's
// annotated input, raw spans or script-type helper.
func treeOracle(body []byte) (int64, []string, error) {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	var count int64
	var nonces []string
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inert bool) {
		inert = inert || n.Type == html.ElementNode && n.Namespace == "" && n.DataAtom == atom.Template
		if n.Type == html.ElementNode && !inert {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				key := a.Key
				if a.Namespace != "" {
					key = a.Namespace + ":" + key
				}
				if _, seen := attrs[key]; !seen {
					attrs[key] = a.Val
				}
			}
			governs := ""
			if (n.Namespace == "" || n.Namespace == "svg") && n.DataAtom == atom.Script {
				typ := strings.ToLower(strings.TrimSpace(attrs["type"]))
				if typ != "" && typ != "module" {
					if parsed, _, e := mime.ParseMediaType(typ); e == nil {
						typ = parsed
					}
				}
				allowed := map[string]bool{
					"": true, "module": true,
					"application/javascript": true, "application/ecmascript": true,
					"application/x-javascript": true, "application/x-ecmascript": true,
					"text/javascript": true, "text/ecmascript": true,
					"text/jscript": true, "text/livescript": true,
					"text/x-javascript": true, "text/x-ecmascript": true,
					"text/javascript1.0": true, "text/javascript1.1": true,
					"text/javascript1.2": true, "text/javascript1.3": true,
					"text/javascript1.4": true, "text/javascript1.5": true,
				}
				if allowed[typ] {
					count++
				}
				governs = "script-src-elem"
			} else if (n.Namespace == "" || n.Namespace == "svg") && n.DataAtom == atom.Style {
				governs = "style-src-elem"
			} else if n.Namespace == "" && n.DataAtom == atom.Link {
				rel := strings.Fields(strings.ToLower(attrs["rel"]))
				for _, r := range rel {
					if r == "stylesheet" || r == "preload" && strings.EqualFold(attrs["as"], "style") {
						governs = "style-src-elem"
						break
					}
					if r == "modulepreload" || r == "preload" && strings.EqualFold(attrs["as"], "script") {
						governs = "script-src-elem"
					}
				}
			}
			if nonce, exists := attrs["nonce"]; exists && governs != "" {
				nonces = append(nonces, n.Namespace+"|"+n.DataAtom.String()+"|"+governs+"|"+nonce)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inert)
		}
	}
	walk(root, false)
	sort.Strings(nonces)
	return count, nonces, nil
}

func htmlSemanticCorpus(t *testing.T) []htmlNormalizationCase {
	rng := rand.New(rand.NewSource(53405))
	corpus := normalizationCorpus(t, rand.New(rand.NewSource(53404)))
	wrappers := [][2]string{
		{"", ""}, {"<svg>", "</svg>"}, {"<math>", "</math>"},
		{"<svg><foreignObject>", "</foreignObject></svg>"},
		{`<math><annotation-xml encoding="text/html">`, "</annotation-xml></math>"},
		{`<math><annotation-xml encoding="application/xhtml+xml">`, "</annotation-xml></math>"},
		{"<template>", "</template>"}, {"<noscript>", "</noscript>"},
		{"<svg><template><foreignObject>", "</foreignObject></template></svg>"},
		{`<math><template><annotation-xml encoding="text/html">`, "</annotation-xml></template></math>"},
		{"<svg><title>", "</title></svg>"},
		{"<table><b><tbody><tr><td>", "</b></td></tr></tbody></table>"},
	}
	types := []string{
		"", "module", "text/javascript", "TEXT/JAVASCRIPT",
		"application/javascript", "text/ecmascript", "application/ecmascript",
		"application/x-javascript", "application/x-ecmascript", "text/jscript",
		"text/livescript", "text/x-javascript", "text/x-ecmascript",
		"text/javascript1.0", "text/javascript1.1", "text/javascript1.2",
		"text/javascript1.3", "text/javascript1.4", "text/javascript1.5",
		"application/json", "importmap", "speculationrules", "text/plain", "invalid",
	}
	for i := 0; i < 384; i++ {
		typ := types[i%len(types)]
		script := fmt.Sprintf(`<script type="%s" nonce="case-%d">app()</script>`, typ, i)
		if i%7 == 0 {
			script = fmt.Sprintf(`<script type="%s" nonce="case-%d" src="">ignored()</script>`, typ, i)
		}
		inside := script + fmt.Sprintf(`<style nonce="style-%d">p{color:red}</style><link nonce="link-%d" rel=stylesheet href="/theme.css">`, i, i)
		for depth := 0; depth < 1+rng.Intn(4); depth++ {
			w := wrappers[rng.Intn(len(wrappers))]
			inside = w[0] + inside + w[1]
		}
		if i%11 == 0 {
			inside = `<iframe srcdoc="` + html.EscapeString(inside) + `"></iframe>`
		}
		corpus = append(corpus, htmlNormalizationCase{name: fmt.Sprintf("tree-%03d", i), body: []byte("<!doctype html><html><head></head><body>" + inside + "</body></html>")})
	}
	// Script raw text and SVG CDATA exercise source correlation independently
	// from namespaces and text-node normalization.
	for i, body := range []string{
		`<svg><script nonce="fixture"><![CDATA[app()]]></script></svg>`,
		`<svg><script nonce="fixture"/></svg>`,
		`<script nonce="fixture">const x="<script>";</script>`,
		`<svg><title><script nonce="fixture">const x="<div";</script></title></svg>`,
		`<noscript><script nonce="fixture">app()</script></noscript><script nonce="fixture">app()</script>`,
		`<script nonce="fixture" type="application/json">{"tag":"<script>"}</script>`,
		`<svg><noscript><foreignObject><script nonce="fixture">app()</script></foreignObject></noscript></svg>`,
		`<svg><script/><script nonce="fixture">app()</script></svg>`,
		`<svg><title><script nonce="fixture">const literal='<script nonce="literal">';app()</script></title></svg>`,
		`<math><script nonce="fixture"/></math>`,
	} {
		corpus = append(corpus, htmlNormalizationCase{name: fmt.Sprintf("raw-%d", i), body: []byte(body)})
	}
	return corpus
}

func TestInlineHTMLTreeDifferentialCorpus(t *testing.T) {
	corpus := htmlSemanticCorpus(t)
	rejected := 0
	for _, item := range corpus {
		t.Run(item.name, func(t *testing.T) {
			wantCount, wantNonces, err := treeOracle(item.body)
			if err != nil {
				t.Fatal(err)
			}
			classified, err := classifyHTML(item.body)
			if !treeOracleConsumesTail(item.body) {
				if err == nil {
					t.Fatal("discarded parser tail accepted")
				}
				rejected++
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var gotCount int64
			var gotNonces []string
			for _, e := range classified.elements {
				if e.executable {
					gotCount++
				}
				if e.hasNonce {
					gotNonces = append(gotNonces, e.namespace+"|"+e.element.String()+"|"+e.directive+"|"+e.nonce)
				}
			}
			sort.Strings(gotNonces)
			if gotCount != wantCount || !reflect.DeepEqual(gotNonces, wantNonces) {
				t.Fatalf("tree classification differs: scripts=%d/%d nonces=%v/%v", gotCount, wantCount, gotNonces, wantNonces)
			}
			measured, err := measureHTML(item.body, HTMLMeasureOptions{}, normalizationIdentity)
			if err != nil {
				t.Fatal(err)
			}
			if measured.ExecutableScripts != wantCount || !bytes.Equal(measured.full, item.body) {
				t.Fatal("measurement differs from tree or raw bytes")
			}
		})
	}
	t.Logf("seed=53405 documents=%d rejected parser tails=%d", len(corpus), rejected)
}

// End evidence is independently checked on the unannotated input. The pinned
// parser's ignore-the-remaining-tokens mode must never produce a valid report.
func treeOracleConsumesTail(body []byte) bool {
	const marker = "oracle-end-evidence"
	root, err := html.Parse(bytes.NewReader(append(bytes.Clone(body), []byte("<!--"+marker+"-->")...)))
	if err != nil {
		return false
	}
	found := false
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.CommentNode && n.Data == marker || n.Type == html.TextNode && strings.Contains(n.Data, "<!--"+marker+"-->") {
			found = true
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}

func TestInlineRejectsDiscardedForeignTemplateTail(t *testing.T) {
	body := []byte(`<svg><foreignObject><template><p>inert</p></template></foreignObject></svg><script nonce="fixture">app()</script>`)
	if treeOracleConsumesTail(body) {
		t.Fatal("parser limitation fixture no longer discards the tail")
	}
	if _, err := measureHTML(body, HTMLMeasureOptions{}, normalizationIdentity); err == nil {
		t.Fatal("measurement accepted a discarded tail")
	}
	if err := VerifyHTMLNonces(body, "script-src 'nonce-fixture'"); err == nil {
		t.Fatal("nonce verification accepted a discarded tail")
	}
}
