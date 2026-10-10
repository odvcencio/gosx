package wire

import (
	"fmt"
	"html"
	"math/rand"
	"strings"
	"testing"
)

func TestReferencesExecutableDataURLsAreIncomplete(t *testing.T) {
	for _, tc := range []struct{ kind, body string }{
		{KindScript, `import "data:text/javascript,import%20%27/hidden.js%27";`},
		{KindScript, `import("data:text/javascript,import%20%27/hidden.js%27");`},
		{KindScript, `new Worker("data:text/javascript,importScripts%28%27/hidden.js%27%29");`},
		{KindStyle, `@import "data:text/css,%40import%20%27/hidden.css%27";`},
		{KindStyle, `@import url(data:text/css,%40import%20%27/hidden.css%27);`},
		{KindStyle, `@import url("data:image/svg+xml,%3Csvg/%3E");`},
		{KindDocument, `<script type="module">import("data:text/javascript,import%20%27/hidden.js%27")</script>`},
		{KindDocument, `<style>@import url(data:text/css,%40import%20%27/hidden.css%27);</style>`},
	} {
		set, err := ScanReferences([]byte(tc.body), tc.kind)
		if err == nil && set.Complete {
			t.Errorf("unscanned executable data claimed complete coverage: %s", tc.body)
		}
	}
}

func TestReferencesOpaqueDataImagesAndFontsStayComplete(t *testing.T) {
	for _, tc := range []struct{ kind, body string }{
		{KindStyle, `.a{background:url(data:image/png;base64,AAAA)}`},
		{KindStyle, `.a{background:url("data:image/svg+xml,%3Csvg/%3E")}`},
		{KindStyle, `@font-face{src:url(data:font/woff2;base64,AAAA)}`},
		{KindDocument, `<img src="data:image/png;base64,AAAA">`},
		{KindDocument, `<link rel="preload" as="image" href="data:image/png;base64,AAAA">`},
		{KindDocument, `<link rel="preload" as="font" href="data:font/woff2;base64,AAAA">`},
	} {
		set, err := ScanReferences([]byte(tc.body), tc.kind)
		if err != nil || !set.Complete || len(set.Resources) != 0 {
			t.Errorf("opaque data changed coverage: %+v %v", set, err)
		}
	}
}

type referenceSchemeCase struct {
	name, target, opaqueKind string
	network                  bool
}

// Fetch Standard sections 4.3 and 6 describe scheme and data fetching:
// https://fetch.spec.whatwg.org/#scheme-fetch
// CSS Values 4 section 4.5 covers URL contexts, including local fragments:
// https://drafts.csswg.org/css-values-4/#urls
// Network references are extracted. Only explicitly opaque image/font data
// can be omitted with complete coverage; other schemes remain unresolved.
func referenceSchemeCases() []referenceSchemeCase {
	rng := rand.New(rand.NewSource(53906))
	suffix := fmt.Sprintf("asset-%08x.png?q=a&b=c#part", rng.Uint32())
	return []referenceSchemeCase{
		{"relative", "./" + suffix, "", true},
		{"root-relative", "/assets/" + suffix, "", true},
		{"protocol-relative", "//example.invalid/" + suffix, "", true},
		{"http", "http://example.invalid/" + suffix, "", true},
		{"https", "https://example.invalid/" + suffix, "", true},
		{"https-case", "HtTpS://example.invalid/" + suffix, "", true},
		{"data-image", "data:image/png;base64,AAAA", KindImage, false},
		{"data-font", "data:font/woff2;base64,AAAA", KindFont, false},
		{"data-script", "data:text/javascript,import%20%27/hidden.js%27", "", false},
		{"data-style", "data:text/css,%40import%20%27/hidden.css%27", "", false},
		{"data-document", "data:text/html,%3Cscript%20src=%27/hidden.js%27%3E%3C/script%3E", "", false},
		{"data-case", "DaTa:IMAGE/png;base64,AAAA", KindImage, false},
		{"blob", "blob:https://example.invalid/asset.png", "", false},
		{"javascript", "javascript:import%28%27/hidden.js%27%29", "", false},
		{"about", "about:blank", "", false},
		{"filesystem", "filesystem:https://example.invalid/temporary/asset.png", "", false},
		{"unknown", "fixture-scheme:asset.png", "", false},
	}
}

// Independent context oracle: a suffix never makes a data module opaque.
func schemePositionOpaque(position referencePosition, scheme referenceSchemeCase) bool {
	if scheme.opaqueKind == "" {
		return false
	}
	if strings.HasPrefix(position.name, "html-") && position.kind == scheme.opaqueKind {
		return true
	}
	switch position.name {
	case "css-image-set", "css-webkit-image-set":
		return scheme.opaqueKind == KindImage
	case "css-image-set-url", "css-url-double", "css-url-single", "css-url-unquoted", "html-inline-style", "html-style-element", "css-font-url":
		return true
	}
	return false
}

func TestReferencesSchemeEncodingCorpus(t *testing.T) {
	positions := generatedReferencePositions()
	positions = append(positions,
		referencePosition{"embedded-import", KindScript, `import TARGET;`, KindScript, false, true, false},
		referencePosition{"embedded-import-call", KindScript, `import(TARGET);`, KindScript, false, false, false})
	seen, failures := map[string]bool{}, map[string]int{}
	count, complete, opaque := 0, 0, 0
	for _, scheme := range referenceSchemeCases() {
		for _, position := range positions {
			var encodings []referenceEncoding
			switch {
			case position.language == KindStyle || position.name == "html-inline-style" || position.name == "html-style-element":
				encodings = generatedCSSEncodings(scheme.target)
			case position.language == KindScript:
				encodings = generatedJSEncodings(scheme.target)
			default:
				encodings = generatedHTMLEncodings(scheme.target)
			}
			for _, encoding := range encodings {
				if position.literal && encoding.expression || position.unquoted && encoding.continuation {
					continue
				}
				value := encoding.text
				if position.name == "html-inline-style" {
					value = html.EscapeString(`background:url("` + value + `")`)
				}
				body, language := strings.ReplaceAll(position.template, "TARGET", value), position.language
				if strings.HasPrefix(position.name, "embedded-") {
					body, language = `<script type="module">`+body+`</script>`, KindDocument
				}
				key := language + "\x00" + body
				if seen[key] {
					continue
				}
				seen[key] = true
				count++
				set, err := ScanReferences([]byte(body), language)
				if !set.Complete {
					continue
				}
				complete++
				valid := err == nil
				if schemePositionOpaque(position, scheme) {
					opaque++
					valid = valid && len(set.Resources) == 0
				} else if !scheme.network {
					valid = false
				} else {
					kind := position.kind
					if kind == "" {
						kind = generatedReferenceKind(scheme.target)
					}
					valid = valid && len(set.Resources) == 1 && referenceValues(set.Resources)[0] == (Reference{URL: scheme.target, Kind: kind, Potential: position.potential})
				}
				if !valid {
					category := scheme.name + "/" + position.name
					if failures[category] == 0 {
						t.Logf("wrong complete result: %s encoding=%s got=%+v", category, encoding.name, set)
					}
					failures[category]++
				}
			}
		}
	}
	t.Logf("seed=53906 schemes=%d contexts=%d sources=%d complete=%d opaque=%d failure-classes=%d", len(referenceSchemeCases()), len(positions), count, complete, opaque, len(failures))
	if count != 30737 || len(failures) > 0 || complete == 0 || opaque == 0 {
		t.Fatalf("scheme corpus disagrees: %v", failures)
	}
}

func TestReferencesHTMLURLAttributeSchemeCorpus(t *testing.T) {
	count, complete, opaque, failures := 0, 0, 0, 0
	for _, row := range htmlURLAttributeCases {
		elements := strings.Fields(row.elements)
		if row.elements == "*" {
			elements = strings.Fields(htmlAttributeIndexElements)
		}
		for _, element := range elements {
			for _, scheme := range referenceSchemeCases() {
				for _, encoding := range generatedHTMLEncodings(scheme.target)[:2] {
					value := scheme.target
					if row.attribute == "style" {
						value = `background:url("` + value + `")`
					} else if row.attribute == "srcdoc" {
						value = `<script src="` + value + `"></script>`
					} else if row.attribute == "content" {
						value = "0; URL=" + value
					} else if row.list {
						value += " 1x, " + scheme.target + " 2x"
					}
					encoded := html.EscapeString(value)
					if encoding.name != "named" {
						encoded = generatedHTMLEncodings(value)[1].text
					}
					attributes := row.attribute + `="` + encoded + `"`
					if element == "input" && row.attribute == "src" {
						attributes += ` type="image"`
					} else if element == "link" {
						attributes += ` rel="stylesheet"`
					} else if row.attribute == "content" {
						attributes += ` http-equiv="refresh"`
					}
					count++
					set, err := ScanReferences([]byte(htmlAttributeDocument(element, attributes)), KindDocument)
					if !set.Complete {
						continue
					}
					complete++
					isOpaque := scheme.opaqueKind != "" && !row.incomplete && (row.attribute == "style" || scheme.opaqueKind == KindImage && row.kind == KindImage)
					valid := err == nil
					if element == "template" {
						valid = valid && len(set.Resources) == 0
					} else if element == "base" && row.attribute == "href" {
						valid = valid && scheme.network && len(set.Resources) == 0 && set.HasBaseHref && set.BaseHref == scheme.target
					} else if isOpaque {
						opaque++
						valid = valid && len(set.Resources) == 0
					} else if !scheme.network || row.incomplete {
						valid = false
					} else {
						valid = valid && len(set.Resources) == 1 && referenceValues(set.Resources)[0] == (Reference{URL: scheme.target, Kind: schemeAttributeKind(row), Potential: false})
					}
					if !valid {
						failures++
						if failures < 10 {
							t.Logf("wrong complete attribute: %s/%s/%s got=%+v", row.attribute, element, scheme.name, set)
						}
					}
				}
			}
		}
	}
	t.Logf("seed=53906 attribute-rows=%d documents=%d complete=%d opaque=%d failures=%d", len(htmlURLAttributeCases), count, complete, opaque, failures)
	if count != 17578 || failures > 0 || complete == 0 || opaque == 0 {
		t.Fatal("HTML scheme corpus disagrees")
	}
}

// The scheme corpus embeds a script in srcdoc; the URL-attribute corpus embeds
// an iframe. Both assert the nested load's actual kind after document traversal.
func schemeAttributeKind(row htmlURLAttributeCase) string {
	if row.attribute == "srcdoc" {
		return KindScript
	}
	return row.kind
}
