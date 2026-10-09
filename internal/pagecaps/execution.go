package pagecaps

import (
	"errors"
	"strings"

	"golang.org/x/net/html"
)

// MaxSrcdocDepth bounds active embedded documents to 32 srcdoc crossings from
// the root (depth zero). Inert templates and sandboxes are not traversed. An
// active document beyond this bound is an input error, never partial evidence.
const MaxSrcdocDepth = 32

// ExecutableSource describes active execution without running it. Body retains
// raw script bytes after srcdoc entity decoding. ExactBody permits matching a
// trusted signature; ambiguous spellings use a conservative byte bound.
type ExecutableSource struct {
	Script, Inline, Synchronous bool
	Body                        []byte
	ExactBody                   bool
}

func walkActiveDocuments(root *html.Node, sources *scriptSources, visit func(*html.Node, map[string]string, int) error, observe func(ExecutableSource)) error {
	type pending struct {
		node    *html.Node
		depth   int
		sources *scriptSources
	}
	queue := []pending{{node: root, sources: sources}}
	for len(queue) > 0 {
		item := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		node := item.node
		attrs := map[string]string{}
		if node.Type == html.ElementNode {
			if node.Namespace == "" && node.Data == "template" {
				continue
			}
			for _, attr := range node.Attr {
				key := strings.ToLower(attr.Key)
				// HTML keeps the first occurrence of a duplicated attribute.
				if _, exists := attrs[key]; !exists {
					attrs[key] = attr.Val
				}
			}
			for key, value := range attrs {
				url := key != "srcdoc" && javascriptURL(value)
				if node.Namespace == "" && node.Data == "iframe" && key == "src" {
					// srcdoc replaces src. Sandbox script restrictions also
					// apply to a javascript: navigation of this child frame.
					_, embedded := attrs["srcdoc"]
					url = url && !embedded && scriptsAllowed(attrs)
				}
				if strings.HasPrefix(key, "on") && len(key) > 2 || url {
					observe(ExecutableSource{Body: []byte(value)})
				}
			}
			if node.Data == "script" && ExecutableScriptType(attrs["type"]) {
				body := item.sources.lookup(node, attrs, scriptText(node))
				_, async := attrs["async"]
				_, deferred := attrs["defer"]
				inline := attrs["src"] == ""
				module := strings.EqualFold(strings.TrimSpace(attrs["type"]), "module")
				observe(ExecutableSource{Script: true, Inline: inline, Synchronous: !module && (inline || !async && !deferred), Body: body.body, ExactBody: body.exact})
			}
			if node.Data == "meta" && strings.EqualFold(strings.TrimSpace(attrs["http-equiv"]), "refresh") && refreshJavascriptURL(attrs["content"]) {
				observe(ExecutableSource{Body: []byte(attrs["content"])})
			}
			if node.Namespace == "" && node.Data == "iframe" && scriptsAllowed(attrs) {
				if content, ok := attrs["srcdoc"]; ok {
					if item.depth >= MaxSrcdocDepth {
						return errors.New("invalid capability HTML")
					}
					child, err := html.Parse(strings.NewReader(content))
					if err != nil {
						return errors.New("invalid capability HTML")
					}
					queue = append(queue, pending{child, item.depth + 1, readScriptSources([]byte(content))})
				}
			}
		}
		if err := visit(node, attrs, item.depth); err != nil {
			return err
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			queue = append(queue, pending{child, item.depth, item.sources})
		}
	}
	return nil
}

func scriptsAllowed(attrs map[string]string) bool {
	sandbox, exists := attrs["sandbox"]
	if !exists {
		return true
	}
	// Sandbox is an ASCII case-insensitive set of space-separated tokens;
	// Unicode whitespace and lookalike letters do not grant script execution.
	for _, token := range strings.FieldsFunc(sandbox, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '\f'
	}) {
		if len(token) == len("allow-scripts") && strings.EqualFold(token, "allow-scripts") {
			return true
		}
	}
	return false
}
