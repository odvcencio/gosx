package budget

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/assetmeasure"
	"m31labs.dev/gosx/internal/pagecaps"
)

// The same markup must produce the same static policy observation at every
// document position. Expected inert/active seeds prevent equal false negatives
// from hiding a broken detector. Case mutations preserve script bodies and
// attribute values, apart from the case-insensitive javascript: scheme.
type executableSeed struct {
	name, markup string
	zeroJS       bool
}

func documentExecutableSeeds() []executableSeed {
	seeds := []executableSeed{}
	for _, typ := range []string{"", "module", "text/javascript", "application/javascript", "text/ecmascript", "application/ecmascript", "application/x-javascript", "application/x-ecmascript", "text/jscript", "text/livescript", "text/x-javascript", "text/x-ecmascript", "text/javascript1.0", "text/javascript1.1", "text/javascript1.2", "text/javascript1.3", "text/javascript1.4", "text/javascript1.5", "text/javascript; charset=utf-8"} {
		seeds = append(seeds, executableSeed{"script-" + typ, `<script type="` + typ + `">window.fixtureReady=1</script>`, false})
	}
	for _, typ := range []string{"application/json", "application/ld+json", "text/plain", "text/markdown", "importmap", "speculationrules"} {
		seeds = append(seeds, executableSeed{"inert-" + typ, `<script type="` + typ + `">{}</script>`, true})
	}
	for _, event := range []string{"onclick", "onload", "onerror", "onfocus", "oninput", "onpointerdown", "ontouchstart", "onanimationend"} {
		seeds = append(seeds, executableSeed{event, `<button ` + event + `="window.fixtureReady=1">Run</button>`, false})
	}
	for _, position := range []struct{ element, attribute string }{{"a", "href"}, {"iframe", "src"}, {"form", "action"}, {"button", "formaction"}} {
		seeds = append(seeds, executableSeed{"url-" + position.attribute, "<" + position.element + " " + position.attribute + `="javascript:window.fixtureReady=1"></` + position.element + ">", false})
	}
	seeds = append(seeds,
		executableSeed{"refresh", `<meta http-equiv="refresh" content="0;url=javascript:window.fixtureReady=1">`, false},
		executableSeed{"refresh-quoted", `<meta http-equiv="refresh" content="0; URL='javascript:window.fixtureReady=1'">`, false},
		executableSeed{"srcdoc-script", `<iframe srcdoc="` + html.EscapeString(`<script>window.fixtureReady=1</script>`) + `"></iframe>`, false},
		executableSeed{"srcdoc-handler", `<iframe srcdoc="` + html.EscapeString(`<button onclick="window.fixtureReady=1">Run</button>`) + `"></iframe>`, false},
		executableSeed{"srcdoc-inert", `<iframe srcdoc="` + html.EscapeString(`<script type="application/json">{}</script>`) + `"></iframe>`, true},
		executableSeed{"svg-script", `<svg><script>window.fixtureReady=1</script></svg>`, false},
		executableSeed{"svg-handler", `<svg onload="window.fixtureReady=1"></svg>`, false},
		executableSeed{"svg-link", `<svg><a href="javascript:window.fixtureReady=1">Run</a></svg>`, false},
		executableSeed{"template", `<template><script>window.fixtureReady=1</script><button onclick="run()">Run</button></template>`, true},
		executableSeed{"template-srcdoc", `<template><iframe srcdoc="` + html.EscapeString(`<script>window.fixtureReady=1</script>`) + `"></iframe></template>`, true},
		executableSeed{"nested-srcdoc", `<iframe srcdoc="` + html.EscapeString(`<iframe srcdoc="`+html.EscapeString(`<button onclick="run()">Run</button>`)+`"></iframe>`) + `"></iframe>`, false},
		executableSeed{"srcdoc-template", `<iframe srcdoc="` + html.EscapeString(`<template><script>window.fixtureReady=1</script></template>`) + `"></iframe>`, true},
		executableSeed{"svg-template", `<svg><template><script>window.fixtureReady=1</script></template></svg>`, false},
		executableSeed{"plain", `<p>Static content</p>`, true},
		executableSeed{"plain-svg", `<svg><text>Static content</text></svg>`, true},
		executableSeed{"plain-link", `<a href="#content">Content</a>`, true})
	return seeds
}

func TestCheckStaticDocumentCorpus(t *testing.T) {
	seeds := documentExecutableSeeds()
	rng := rand.New(rand.NewSource(55002))
	dir := t.TempDir()
	info := publicTestReport(t).Info
	// Reuse real compressor results for repeated bodies. The corpus exercises
	// document execution classification, not repeated compressor invocations.
	cache := map[string]assetmeasure.Sizes{}
	normalize := func(body []byte) (assetmeasure.Sizes, error) {
		key := string(body)
		if sizes, ok := cache[key]; ok {
			return sizes, nil
		}
		sizes, err := testBodyNormalizer(body)
		if err == nil {
			cache[key] = sizes
		}
		return sizes, err
	}
	for i, s := range seeds {
		for variant := 0; variant < 8; variant++ {
			markup := documentSeedVariant(s, variant, rng)
			t.Run(fmt.Sprintf("%02d-%s/%d", i, s.name, variant), func(t *testing.T) {
				root := measureCorpusDocument(t, dir, info, markup, false, normalize)
				child := measureCorpusDocument(t, dir, info, markup, true, normalize)
				if root != child {
					t.Errorf("root/child zero-JS disagreement: root=%v child=%v", root, child)
				}
				if root != s.zeroJS || child != s.zeroJS {
					t.Errorf("active/inert seed verdict changed: want=%v root=%v child=%v", s.zeroJS, root, child)
				}
			})
		}
	}
	t.Logf("fixed-seed document corpus: %d snippets, %d root/child measurements", len(seeds)*8, len(seeds)*16)
}

func documentSeedVariant(s executableSeed, variant int, rng *rand.Rand) string {
	markup := s.markup
	if variant > 0 {
		// Mutate only ASCII markup identifiers and the URL scheme.
		for _, token := range []string{"script", "type", "button", "onclick", "onload", "onerror", "onfocus", "oninput", "onpointerdown", "ontouchstart", "onanimationend", "href", "srcdoc", "src", "http-equiv", "refresh", "content", "action", "formaction", "javascript:"} {
			mixed := strings.Map(func(r rune) rune {
				if r >= 'a' && r <= 'z' && rng.Intn(2) == 0 {
					return r - 32
				}
				return r
			}, token)
			// Foreign SVG element names are case-sensitive; only
			// mutate HTML tags, or attributes/schemes within SVG.
			if !strings.HasPrefix(markup, "<svg") || token != "script" {
				markup = strings.ReplaceAll(markup, token, mixed)
			}
		}
	}
	return markup
}

func measureCorpusDocument(t *testing.T, dir string, info PublicInfo, snippet string, asChild bool, normalize bodyNormalizer) bool {
	t.Helper()
	report := measureCorpusReport(t, dir, info, snippet, asChild, normalize)
	for _, row := range report.Rows {
		if row.PageType == "static" {
			for _, policy := range row.Policies {
				if policy.Name == "zero-js" {
					return policy.Passed
				}
			}
		}
	}
	t.Fatal("static zero-JS policy observation missing")
	return false
}

func measureCorpusReport(t *testing.T, dir string, info PublicInfo, snippet string, asChild bool, normalize bodyNormalizer) AppReport {
	t.Helper()
	root, child := snippet, "<p>Unused child</p>"
	if asChild {
		root, child = `<iframe src="/child/"></iframe>`, snippet
	}
	return measureCorpusDocuments(t, dir, info, root, child, normalize)
}

func measureCorpusDocuments(t *testing.T, dir string, info PublicInfo, rootSnippet, childSnippet string, normalize bodyNormalizer) AppReport {
	t.Helper()
	wrap := func(content string) []byte { return []byte("<!doctype html><html><body>" + content + "</body></html>") }
	root, child := wrap(rootSnippet), wrap(childSnippet)
	caps, err := pagecaps.FromHTML(root)
	if err != nil {
		t.Fatal("root capability parsing failed", err)
	}
	manifest := &FixtureManifest{Schema: "gosx.perf-fixtures/v1", Version: 1, SourceSHA: info.SHA, CatalogSHA256: info.FixtureSHA256,
		Routes: []FixtureRoute{{App: "fixture", RouteTemplate: "/counter/", SourcePath: "fixture/page.gsx", PageTypes: []string{"static", "enhanced"}, Capabilities: caps, CriticalAssetIDs: []string{"app/fixture/html"}, InputSequenceID: "counter-input"}},
		Assets: []buildmanifest.PerfAssetUse{
			{ID: "app/fixture/html", URL: "/counter/", Kind: "html", Owner: "app", SHA256: testMeasureHash(root), Phase: "critical", Condition: "always", Dependencies: []string{}},
			{ID: "app/fixture/child", URL: "/child/", Kind: "html", Owner: "app", SHA256: testMeasureHash(child), Phase: "dormant", Condition: "always", Dependencies: []string{}},
		}}
	for name, body := range map[string][]byte{"counter/index.html": root, "child/index.html": child} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFixtureManifest(t, dir, manifest)
	info.ArtifactSHA256 = &manifest.FixturesSHA256
	client := &http.Client{Transport: testRoundTrip(func(req *http.Request) (*http.Response, error) {
		body := root
		if req.URL.Path == "/child/" {
			// Unknown closure can conservatively fetch the unused inert body.
			// It must not change the snippet's executable observation.
			body = child
		} else if req.URL.Path != "/counter/" {
			t.Errorf("undeclared request in corpus: %s", req.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}, nil
	})}
	report, err := measureApp(context.Background(), MeasureOptions{App: "fixture", DistDir: dir, BaseURL: "https://example.invalid", Client: client, Public: info}, normalize)
	if err != nil {
		t.Fatal("corpus document measurement failed", err)
	}
	return report
}
