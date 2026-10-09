package pagecaps

import (
	"strings"

	"golang.org/x/net/html"
)

// MaxSrcdocDepth bounds active embedded documents to 32 srcdoc crossings from
// the root (depth zero). Templates are inert; sandboxes disable execution but
// retain live resource edges. Active inline evidence beyond this bound fails.
const MaxSrcdocDepth = 32

// ExecutableSource describes active execution without running it. Body retains
// raw script bytes after srcdoc entity decoding. ExactBody permits matching a
// trusted signature; ambiguous spellings use a conservative byte bound.
type ExecutableSource struct {
	Script, Inline, Synchronous bool
	Body                        []byte
	ExactBody                   bool
}

func walkActiveDocuments(tree *DocumentTree, visit func(*html.Node, map[string]string, int) error, observe func(*Document, ExecutableSource)) error {
	seen := map[string]bool{}
	for _, doc := range tree.Documents {
		if !doc.ScriptsAllowed || seen[doc.Key] {
			continue
		}
		seen[doc.Key] = true
		if err := doc.Walk(func(node *html.Node, attrs map[string]string, _ int) error {
			if node.Type == html.ElementNode {
				for key, value := range attrs {
					url := key != "srcdoc" && javascriptURL(value)
					if node.Namespace == "" && node.Data == "iframe" && key == "src" {
						_, embedded := attrs["srcdoc"]
						url = url && !embedded && scriptsAllowed(attrs)
					}
					if strings.HasPrefix(key, "on") && len(key) > 2 || url {
						observe(doc, ExecutableSource{Body: []byte(value)})
					}
				}
				if node.Data == "script" && ExecutableScriptType(attrs["type"]) {
					body := doc.sources.lookup(node, attrs, scriptText(node))
					_, async := attrs["async"]
					_, deferred := attrs["defer"]
					inline := attrs["src"] == ""
					module := strings.EqualFold(strings.TrimSpace(attrs["type"]), "module")
					observe(doc, ExecutableSource{Script: true, Inline: inline, Synchronous: !module && (inline || !async && !deferred), Body: body.body, ExactBody: body.exact})
				}
				if node.Data == "meta" && strings.EqualFold(strings.TrimSpace(attrs["http-equiv"]), "refresh") && refreshJavascriptURL(attrs["content"]) {
					observe(doc, ExecutableSource{Body: []byte(attrs["content"])})
				}
			}
			depth := doc.Depth - tree.Root.Depth
			return visit(node, attrs, depth)
		}); err != nil {
			return err
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
