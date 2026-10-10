package pagecaps

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

type scriptKey struct{ namespace, text, typ, src string }
type scriptBody struct {
	body  []byte
	exact bool
}
type rawScript struct {
	body     []byte
	typ, src string
}
type scriptSources struct {
	raw     []rawScript
	html    map[scriptKey]scriptBody
	foreign map[string]map[scriptKey]scriptBody
}

// Raw spelling matters to byte guardrails and build signatures even when HTML
// parsing normalizes CRLF, character references or CDATA. This lexer only
// retains source bytes; the active DOM traversal decides what executes.
func readScriptSources(data []byte) *scriptSources {
	sources := &scriptSources{html: map[scriptKey]scriptBody{}}
	lexer := html.NewTokenizer(bytes.NewReader(data))
	var body bytes.Buffer
	active, typ, src := false, "", ""
	finish := func() {
		raw := bytes.Clone(body.Bytes())
		sources.raw = append(sources.raw, rawScript{raw, typ, src})
		text := strings.NewReplacer("\r\n", "\n", "\r", "\n", "\x00", "\ufffd").Replace(string(raw))
		sources.add(sources.html, scriptKey{"", text, typ, src}, raw)
		body.Reset()
		active = false
	}
	for {
		kind := lexer.Next()
		raw := bytes.Clone(lexer.Raw())
		if kind == html.ErrorToken {
			if active {
				body.Write(raw)
				finish()
			}
			return sources
		}
		if active {
			if kind == html.EndTagToken && lexer.Token().Data == "script" {
				finish()
			} else {
				body.Write(raw)
			}
			continue
		}
		if kind == html.StartTagToken || kind == html.SelfClosingTagToken {
			token := lexer.Token()
			if token.Data != "script" {
				continue
			}
			attrs := map[string]string{}
			for _, attr := range token.Attr {
				if _, exists := attrs[attr.Key]; !exists {
					attrs[attr.Key] = attr.Val
				}
			}
			active, typ, src = true, attrs["type"], attrs["src"]
		}
	}
}

func (*scriptSources) add(index map[scriptKey]scriptBody, key scriptKey, raw []byte) {
	if old, exists := index[key]; exists {
		if !bytes.Equal(old.body, raw) {
			// Ambiguous DOM spellings retain the larger source-byte bound and
			// cannot establish a trusted framework signature.
			old.exact = false
			if len(raw) > len(old.body) {
				old.body = raw
			}
			index[key] = old
		}
	} else {
		index[key] = scriptBody{raw, true}
	}
}

func (sources *scriptSources) lookup(node *html.Node, attrs map[string]string, text string) scriptBody {
	index := sources.html
	if node.Namespace != "" {
		if sources.foreign == nil {
			sources.foreign = map[string]map[scriptKey]scriptBody{}
		}
		index = sources.foreign[node.Namespace]
		if index == nil {
			index = map[scriptKey]scriptBody{}
			sources.foreign[node.Namespace] = index
			tag := node.Namespace
			for _, raw := range sources.raw {
				root, _ := html.Parse(strings.NewReader("<" + tag + "><script>" + string(raw.body) + "</script></" + tag + ">"))
				var find func(*html.Node) *html.Node
				find = func(n *html.Node) *html.Node {
					if n.Namespace == node.Namespace && n.Data == "script" {
						return n
					}
					for child := n.FirstChild; child != nil; child = child.NextSibling {
						if found := find(child); found != nil {
							return found
						}
					}
					return nil
				}
				if script := find(root); script != nil {
					sources.add(index, scriptKey{node.Namespace, scriptText(script), raw.typ, raw.src}, raw.body)
				}
			}
		}
	}
	if body, exists := index[scriptKey{node.Namespace, text, attrs["type"], attrs["src"]}]; exists {
		return body
	}
	return scriptBody{body: []byte(text)}
}

func scriptText(node *html.Node) string {
	var body strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			body.WriteString(child.Data)
		}
	}
	return body.String()
}
