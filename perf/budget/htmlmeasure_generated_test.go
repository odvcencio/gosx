package budget

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/assetmeasure"
)

func TestInlineRejectsIncompleteTrailingMarkup(t *testing.T) {
	complete := []byte(`<!doctype html><html><body><p>Stable</p></body></html>`)
	result, err := measureHTML(complete, HTMLMeasureOptions{}, testBodyNormalizer)
	if err != nil || !bytes.Equal(result.full, complete) {
		t.Fatal("complete document changed", err)
	}
	for _, suffix := range []string{"<di", "<spa", "</di", `<div title="unfinished`, "<div title='unfinished", "<div title=unfinished"} {
		t.Run(suffix, func(t *testing.T) {
			body := append(bytes.Clone(complete), suffix...)
			_, err := measureHTML(body, HTMLMeasureOptions{}, testBodyNormalizer)
			var input *InputError
			if !errors.As(err, &input) || input.Code != "wrong-fixture" || input.Pointer != "/html" {
				t.Fatal("incomplete suffix must be rejected as invalid HTML", err)
			}
		})
	}
}

type htmlValueSpan struct{ start, end int }
type htmlNormalizationCase struct {
	name    string
	body    []byte
	ignored []htmlValueSpan
}

// Only values of these explicitly declared fields may change without changing
// render identity. Tag syntax and attribute names,
// including the value's surrounding quotes, must remain byte-identical.
func normalizationCorpusOptions() HTMLMeasureOptions {
	return HTMLMeasureOptions{Fields: []HTMLField{
		{"script", "nonce"}, {"style", "nonce"}, {"link", "nonce"},
		{"html", "data-gosx-session"}, {"body", "data-gosx-session"}, {"meta", "data-gosx-session"},
		{"html", "data-gosx-build-timestamp"}, {"body", "data-gosx-build-timestamp"}, {"meta", "data-gosx-build-timestamp"},
	}}
}

func normalizationCorpus(t *testing.T, random *rand.Rand) []htmlNormalizationCase {
	t.Helper()
	var corpus []htmlNormalizationCase
	for _, name := range []string{"inline.html", "inline-normalized.html"} {
		body, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		item := htmlNormalizationCase{name: name, body: body}
		// Known fixture value spans, independently of the production tokenizer.
		for _, value := range []string{"nonce-first", "session-first", "gosx-normalized"} {
			for offset := 0; offset < len(body); {
				i := bytes.Index(body[offset:], []byte(value))
				if i < 0 {
					break
				}
				i += offset
				item.ignored = append(item.ignored, htmlValueSpan{i, i + len(value)})
				offset = i + len(value)
			}
		}
		sort.Slice(item.ignored, func(i, j int) bool { return item.ignored[i].start < item.ignored[j].start })
		corpus = append(corpus, item)
	}
	contents := []string{
		`<p class="message" title='a &amp; b' data-count=7>Text &lt; &gt; &#65; &#x1F30D; 🌍</p>`,
		`<!-- comment <script>inert()</script> --><p hidden>Comment fixture</p>`,
		`<svg viewBox="0 0 10 10"><![CDATA[<g>&text]]><text x='1'>SVG</text></svg>`,
		`<textarea name=message>&lt;script&gt; &amp; text</textarea><title>Raw &amp; title</title>`,
		`<xmp><p>raw <script>text</script></p></xmp><iframe>fallback &amp; text</iframe>`,
		`<template><script>inert()</script><p title="template">Slot</p></template>`,
		`<script type="application/json">{"escaped":"<tag>","value":7}</script><pre>\t spaces\n</pre>`,
		`<noscript><p>Fallback</p></noscript><input disabled data-note="nonce=stable">`,
	}
	for variant, content := range contents {
		for _, quote := range []string{"\"", "'", ""} {
			var source strings.Builder
			var spans []htmlValueSpan
			attribute := func(name string) {
				fmt.Fprintf(&source, " %s=%s", name, quote)
				start := source.Len()
				fmt.Fprintf(&source, "value-%08x", random.Uint32())
				spans = append(spans, htmlValueSpan{start, source.Len()})
				source.WriteString(quote)
			}
			source.WriteString("<!doctype html><html lang=en")
			attribute("data-gosx-session")
			attribute("data-gosx-build-timestamp")
			source.WriteString("><head><meta")
			attribute("data-gosx-session")
			attribute("data-gosx-build-timestamp")
			source.WriteString("><link rel=stylesheet href='/theme.css?mode=dark#v1'")
			attribute("nonce")
			source.WriteString("><style")
			attribute("nonce")
			source.WriteString(">p::before{content:'< & >';}p{color:red}</style><script")
			attribute("nonce")
			source.WriteString(">const value = '< & >'; /* stable */ app(value);</script></head><body")
			attribute("data-gosx-session")
			attribute("data-gosx-build-timestamp")
			source.WriteString(">")
			source.WriteString(content)
			source.WriteString("</body></html>\n")
			corpus = append(corpus, htmlNormalizationCase{
				name: fmt.Sprintf("generated-%d-quote-%q", variant, quote), body: []byte(source.String()), ignored: spans,
			})
		}
	}
	return corpus
}

// The oracle operates on known source spans, not HTML parsing. Compression is
// irrelevant to byte preservation; avoid recompressing every generated mutant.
func normalizationIdentity(body []byte) (assetmeasure.Sizes, error) {
	return assetmeasure.Sizes{Raw: int64(len(body)), SHA256: testMeasureHash(body)}, nil
}

func TestInlineNormalizationPreservesGeneratedDocuments(t *testing.T) {
	random := rand.New(rand.NewSource(53404))
	corpus := normalizationCorpus(t, random)
	truncations, mutations, ignoredMutations := 0, 0, 0
	for _, item := range corpus {
		for _, declared := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/declared-%t", item.name, declared), func(t *testing.T) {
				opts := HTMLMeasureOptions{}
				mask := make([]bool, len(item.body))
				var expected bytes.Buffer
				if declared {
					opts = normalizationCorpusOptions()
					offset := 0
					for _, span := range item.ignored {
						expected.Write(item.body[offset:span.start])
						expected.WriteString("gosx-normalized")
						for i := span.start; i < span.end; i++ {
							mask[i] = true
						}
						offset = span.end
					}
					expected.Write(item.body[offset:])
				} else {
					expected.Write(item.body)
				}
				original, err := measureHTML(item.body, opts, normalizationIdentity)
				if err != nil || !bytes.Equal(original.full, expected.Bytes()) {
					t.Fatal("original differs from source-span oracle", err)
				}
				failures := 0
				assertChanged := func(body []byte, kind string, offset int) {
					result, err := measureHTML(body, opts, normalizationIdentity)
					if err == nil && VerifyHTMLRenders(original, result) == nil {
						if failures < 8 {
							t.Errorf("%s at byte %d lost a source difference", kind, offset)
						}
						failures++
					}
				}
				for offset := 0; offset < len(item.body); offset++ {
					truncations++
					assertChanged(item.body[:offset], "truncation", offset)
					if mask[offset] {
						// An opaque alphanumeric substitution stays within the value;
						// changing a delimiter would change the attribute's syntax.
						changed := bytes.Clone(item.body)
						changed[offset] = 'Z'
						if changed[offset] == item.body[offset] {
							changed[offset] = 'Y'
						}
						ignoredMutations++
						result, err := measureHTML(changed, opts, normalizationIdentity)
						if err != nil || VerifyHTMLRenders(original, result) != nil {
							t.Fatalf("ignored value mutation at byte %d changed identity: %v", offset, err)
						}
						continue
					}
					// Exercise every offset with syntax, controls, case changes and
					// fixed-seed arbitrary bytes, including invalid UTF-8.
					for _, replacement := range []byte{'<', '>', '/', '\'', '"', '&', '=', ' ', '\t', '\n', 0, item.body[offset] ^ 0x20, byte(random.Intn(256))} {
						if replacement == item.body[offset] {
							continue
						}
						changed := bytes.Clone(item.body)
						changed[offset] = replacement
						mutations++
						assertChanged(changed, "mutation", offset)
					}
				}
				if failures > 0 {
					t.Errorf("%d source differences lost", failures)
				}
			})
		}
	}
	t.Logf("seed=53404 documents=%d configurations=%d truncations=%d mutations=%d ignored-value mutations=%d", len(corpus), 2*len(corpus), truncations, mutations, ignoredMutations)
}
