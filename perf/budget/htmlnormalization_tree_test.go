package budget

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestInlineForeignScriptNormalization(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"ordinary", `<script nonce="first">app()</script>`},
		{"svg-title", `<svg><title><script nonce="first">app()</script></title></svg>`},
		{"svg-self-closing", `<svg><script/><script nonce="first">app()</script></svg>`},
		{"math-annotation", `<math><annotation-xml encoding="text/html"><script nonce="first">app()</script></annotation-xml></math>`},
		{"foreign-object", `<svg><foreignObject><script nonce="first">app()</script></foreignObject></svg>`},
		{"literal-script", `<svg><title><script nonce="first">const literal='<script nonce="literal">';app()</script></title></svg>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := HTMLMeasureOptions{Fields: []HTMLField{{"script", "nonce"}}, FrameworkScriptSHA256: []string{testMeasureHash([]byte("app()"))}}
			first, err := measureHTML([]byte(tc.body), opts, normalizationIdentity)
			if err != nil || first.ExecutableScripts == 0 {
				t.Fatal("script not measured", err)
			}
			changed := strings.Replace(tc.body, `nonce="first"`, `nonce="second"`, 1)
			second, err := measureHTML([]byte(changed), opts, normalizationIdentity)
			if err != nil || VerifyHTMLRenders(first, second) != nil || !bytes.Equal(first.withoutFramework, second.withoutFramework) {
				t.Error("declared nonce changed normalized documents", err)
			}
			if strings.Contains(tc.body, `nonce="literal"`) && !bytes.Contains(first.full, []byte(`nonce="literal"`)) {
				t.Error("literal script text changed")
			}
			for _, edit := range [][2]string{{"app()", "app(1)"}, {`nonce="literal"`, `nonce="changed"`}} {
				if !strings.Contains(tc.body, edit[0]) {
					continue
				}
				mutant, err := measureHTML([]byte(strings.Replace(tc.body, edit[0], edit[1], 1)), opts, normalizationIdentity)
				if err == nil && VerifyHTMLRenders(first, mutant) == nil {
					t.Error("literal script change lost")
				}
			}
		})
	}
}

// The unannotated HTML5 tree decides whether an independently marked source
// value is an actual declared attribute. A match in script text, noscript or
// an encoded srcdoc string is not an attribute in this document's tree.
func treeOracleDeclaredValue(body []byte, opts HTMLMeasureOptions, marker string) bool {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return false
	}
	found := false
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				for _, field := range opts.Fields {
					if a.Namespace == "" && n.Data == field.Element && a.Key == field.Attribute && a.Val == marker {
						found = true
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}

func TestInlineHTMLTreeNormalizationCorpus(t *testing.T) {
	corpus := htmlSemanticCorpus(t)
	opts := normalizationCorpusOptions()
	// This only finds candidate byte spans. Tree construction, independently
	// of the production source association, determines whether each is ignored.
	candidates := regexp.MustCompile(`(?i)(?:nonce|data-gosx-session|data-gosx-build-timestamp)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>"'&]+))`)
	accepted, rejected, ignored, retained, mutations, disagreements := 0, 0, 0, 0, 0, 0
	for _, item := range corpus {
		t.Run(item.name, func(t *testing.T) {
			original, err := measureHTML(item.body, opts, normalizationIdentity)
			if !treeOracleConsumesTail(item.body) {
				if err == nil {
					t.Error("discarded tree tail accepted")
				}
				rejected++
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			accepted++
			mask := make([]bool, len(item.body))
			failures := 0
			fail := func(message string, offset int) {
				if failures < 3 {
					t.Errorf("%s at byte %d", message, offset)
				}
				failures++
				disagreements++
			}
			for ordinal, match := range candidates.FindAllSubmatchIndex(item.body, -1) {
				start, end := -1, -1
				for group := 2; group < len(match); group += 2 {
					if match[group] >= 0 {
						start, end = match[group], match[group+1]
						break
					}
				}
				marker := fmt.Sprintf("oracle-value-%d", ordinal)
				changed := append(bytes.Clone(item.body[:start]), []byte(marker)...)
				changed = append(changed, item.body[end:]...)
				declared := treeOracleDeclaredValue(changed, opts, marker)
				if declared {
					ignored++
					for i := start; i < end; i++ {
						mask[i] = true
					}
				} else {
					retained++
				}
				result, err := measureHTML(changed, opts, normalizationIdentity)
				if declared {
					if err != nil || VerifyHTMLRenders(original, result) != nil || !bytes.Equal(original.withoutFramework, result.withoutFramework) {
						fail("declared tree attribute changed render identity", start)
					}
				} else if err == nil && VerifyHTMLRenders(original, result) == nil {
					fail("literal attribute-shaped bytes lost", start)
				}
			}
			for i := range item.body {
				if mask[i] {
					continue
				}
				changed := bytes.Clone(item.body)
				changed[i] ^= 0x20
				mutations++
				result, err := measureHTML(changed, opts, normalizationIdentity)
				if err == nil && VerifyHTMLRenders(original, result) == nil {
					fail("undeclared byte mutation lost", i)
				}
			}
		})
	}
	t.Logf("seed=53405 documents=%d accepted=%d rejected=%d declared changes=%d literal changes=%d undeclared mutations=%d disagreements=%d", len(corpus), accepted, rejected, ignored, retained, mutations, disagreements)
}
