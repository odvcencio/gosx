package budget

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"m31labs.dev/gosx/internal/pagecaps"
)

type htmlSourceToken struct {
	kind       html.TokenType
	token      html.Token
	start, end int
}
type htmlElement struct {
	element                                     atom.Atom
	namespace, nonce, text, directive, fallback string
	hasNonce, external, executable              bool
	bodyStart, bodyEnd                          int
}
type htmlClassification struct {
	starts     []htmlSourceToken
	elements   []htmlElement
	executable bool
}

// Tree construction owns element semantics. Source markers only associate
// tree elements with raw byte spans; they never enter measured bytes or CSP
// text. Speculative lexical spans can identify tags inside foreign raw-text
// contexts, but only nodes created by the tree builder become elements.
func classifyHTML(body []byte) (htmlClassification, error) {
	var result htmlClassification
	if len(body) > 16<<20 || !utf8.Valid(body) {
		return result, measureFailure("wrong-fixture", "/html")
	}
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	starts := map[int]htmlSourceToken{}
	offset := 0
	for {
		kind := tokenizer.Next()
		raw := tokenizer.Raw()
		if kind == html.ErrorToken {
			if tokenizer.Err() != io.EOF || len(raw) != 0 {
				return result, measureFailure("wrong-fixture", "/html")
			}
			break
		}
		item := htmlSourceToken{kind: kind, start: offset, end: offset + len(raw)}
		if kind == html.StartTagToken || kind == html.SelfClosingTagToken || kind == html.EndTagToken {
			item.token = tokenizer.Token()
			seen := map[string]bool{}
			for _, attr := range item.token.Attr {
				if seen[attr.Key] {
					return result, measureFailure("wrong-fixture", "/html/attributes")
				}
				seen[attr.Key] = true
			}
			if kind != html.EndTagToken && htmlNormalizedElement(item.token.DataAtom) {
				starts[offset] = item
			}
		}
		offset = item.end
	}
	// This pass records possible source positions, not namespaces or activity.
	// It must not hide a real script inside e.g. an SVG title. Invalid speculative
	// tokens remain raw bytes and cannot change the authoritative token stream.
	tokenizer = html.NewTokenizer(bytes.NewReader(body))
	tokenizer.AllowCDATA(true)
	offset = 0
	for {
		tokenizer.NextIsNotRawText()
		kind := tokenizer.Next()
		raw := tokenizer.Raw()
		if kind == html.ErrorToken {
			break
		}
		if kind == html.StartTagToken || kind == html.SelfClosingTagToken {
			token := tokenizer.Token()
			if htmlNormalizedElement(token.DataAtom) {
				starts[offset] = htmlSourceToken{kind, token, offset, offset + len(raw)}
			}
		}
		offset += len(raw)
	}
	digest := sha256.Sum256(body)
	suffix := hex.EncodeToString(digest[:16])
	marker := "data-gosx-budget-" + suffix
	tail := "gosx-budget-end-" + suffix
	if bytes.Contains(bytes.ToLower(body), []byte(marker)) || bytes.Contains(body, []byte(tail)) {
		return result, measureFailure("wrong-fixture", "/html")
	}
	// Apply positions in source order, preserving every original byte. A marker
	// in literal raw text is removed from tree text before nonce/hash checking.
	var annotated bytes.Buffer
	var replacements []string
	for i := 0; i < len(body); {
		item, found := starts[i]
		if !found {
			annotated.WriteByte(body[i])
			i++
			continue
		}
		stop := item.end - 1
		if item.kind == html.SelfClosingTagToken {
			stop--
		}
		// Distinct keys survive html/body attribute merging. A shared key
		// would retain only the first root tag's source position.
		position := strconv.Itoa(i)
		insertion := " " + marker + "-" + position + "=" + position + " "
		annotated.Write(body[i:stop])
		annotated.WriteString(insertion)
		annotated.Write(body[stop:item.end])
		replacements = append(replacements, insertion, "")
		i = item.end
	}
	sentinel := "<!--" + tail + "-->"
	annotated.WriteString(sentinel)
	replacements = append(replacements, sentinel, "")
	replacer := strings.NewReplacer(replacements...)
	root, err := html.ParseWithOptions(bytes.NewReader(annotated.Bytes()), html.ParseOptionEnableScripting(true))
	if err != nil {
		return result, measureFailure("wrong-fixture", "/html")
	}
	strip := replacer.Replace
	consumed := false
	associated := map[int]bool{}
	err = pagecaps.WalkHTML(root, func(node *html.Node, state pagecaps.HTMLState) error {
		if node.Type == html.CommentNode && node.Data == tail || node.Type == html.TextNode && strings.Contains(node.Data, sentinel) {
			consumed = true
		}
		result.executable = result.executable || state.Executable
		if node.Type == html.ElementNode && htmlNormalizedElement(node.DataAtom) {
			attrs := map[string]string{}
			var sources []htmlSourceToken
			for _, attr := range node.Attr {
				if attr.Namespace == "" && strings.HasPrefix(attr.Key, marker+"-") {
					start, e := strconv.Atoi(attr.Val)
					if e != nil {
						return measureFailure("wrong-fixture", "/html")
					}
					if source, found := starts[start]; found {
						sources = append(sources, source)
					}
					continue
				}
				key := attr.Key
				if attr.Namespace != "" {
					key = attr.Namespace + ":" + key
				}
				if _, exists := attrs[key]; !exists {
					attrs[key] = strip(attr.Val)
				}
			}
			sort.Slice(sources, func(i, j int) bool { return sources[i].start < sources[j].start })
			contributed := map[string]bool{}
			for _, source := range sources {
				if source.token.DataAtom != node.DataAtom {
					return measureFailure("wrong-fixture", "/html")
				}
				seen := map[string]bool{}
				for _, attr := range source.token.Attr {
					if seen[attr.Key] {
						return measureFailure("wrong-fixture", "/html/attributes")
					}
					seen[attr.Key] = true
				}
				if node.Namespace == "" && (node.DataAtom == atom.Html || node.DataAtom == atom.Body) {
					// Root attributes merge only when their name is absent. Keep
					// ignored later values literal, even on an accepted root tag.
					var merged []html.Attribute
					for _, attr := range source.token.Attr {
						if !contributed[attr.Key] {
							merged = append(merged, attr)
							contributed[attr.Key] = true
						}
					}
					source.token.Attr = merged
				}
				if !associated[source.start] {
					result.starts = append(result.starts, source)
					associated[source.start] = true
				}
			}
			source, found := htmlSourceToken{}, len(sources) > 0
			if found {
				source = sources[0]
			}
			var text strings.Builder
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.TextNode {
					text.WriteString(child.Data)
				}
			}
			element := htmlElement{element: node.DataAtom, namespace: node.Namespace, text: strip(text.String())}
			element.nonce, element.hasNonce = attrs["nonce"]
			switch node.DataAtom {
			case atom.Script:
				if node.Namespace != "" && node.Namespace != "svg" {
					break
				}
				element.directive, element.fallback = "script-src-elem", "script-src"
				element.executable = state.Script
				_, element.external = attrs["src"]
				if node.Namespace == "svg" {
					_, href := attrs["href"]
					_, xlink := attrs["xlink:href"]
					element.external = href || xlink
				}
				if !found {
					return measureFailure("wrong-fixture", "/html")
				}
				element.bodyStart, element.bodyEnd, err = htmlScriptSource(body, source, node.Namespace)
				if err != nil {
					return err
				}
			case atom.Style:
				element.directive, element.fallback = "style-src-elem", "style-src"
			case atom.Link:
				if node.Namespace != "" {
					break
				}
				for _, rel := range strings.Fields(cspLower(attrs["rel"])) {
					if rel == "stylesheet" || rel == "preload" && strings.EqualFold(attrs["as"], "style") {
						element.directive, element.fallback = "style-src-elem", "style-src"
						break
					}
					if rel == "modulepreload" || rel == "preload" && strings.EqualFold(attrs["as"], "script") {
						element.directive, element.fallback = "script-src-elem", "script-src"
					}
				}
				element.external = true
			}
			if !state.Inert && element.directive != "" && (node.Namespace == "" || node.Namespace == "svg") {
				result.elements = append(result.elements, element)
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	// The pinned tree builder explicitly ignores remaining tokens in some
	// foreign/template combinations. Missing end evidence must fail closed.
	if !consumed {
		return result, measureFailure("wrong-fixture", "/html")
	}
	sort.Slice(result.starts, func(i, j int) bool { return result.starts[i].start < result.starts[j].start })
	return result, nil
}

// These are the only elements with declarable transient attributes. A start
// span enters normalization only after it is associated with a real tree node;
// tag-shaped bytes in script text, noscript and srcdoc remain literal bytes.
func htmlNormalizedElement(element atom.Atom) bool {
	return htmlSourceElement(element) || element == atom.Html || element == atom.Body || element == atom.Meta
}

func htmlSourceElement(element atom.Atom) bool {
	return element == atom.Script || element == atom.Style || element == atom.Link
}

// Only raw span accounting happens here. The tree has already selected the
// namespace, script kind and activity; tokenizer tag names cannot declare a
// script executable or a template inert.
func htmlScriptSource(body []byte, source htmlSourceToken, namespace string) (int, int, error) {
	if namespace == "svg" && source.kind == html.SelfClosingTagToken {
		return source.end, source.end, nil
	}
	tokenizer := html.NewTokenizerFragment(bytes.NewReader(body[source.end:]), "script")
	if namespace == "svg" {
		tokenizer = html.NewTokenizer(bytes.NewReader(body[source.end:]))
		tokenizer.AllowCDATA(true)
	}
	offset, depth := source.end, 1
	for {
		if namespace == "svg" {
			tokenizer.NextIsNotRawText()
		}
		kind := tokenizer.Next()
		raw := tokenizer.Raw()
		if kind == html.ErrorToken {
			return 0, 0, measureFailure("wrong-fixture", "/html")
		}
		if kind == html.EndTagToken && tokenizer.Token().DataAtom == atom.Script {
			depth--
			if depth == 0 {
				return source.end, offset, nil
			}
		} else if namespace == "svg" && kind == html.StartTagToken && tokenizer.Token().DataAtom == atom.Script {
			depth++
		}
		offset += len(raw)
	}
}
