package pagecaps

import (
	"strings"

	"golang.org/x/net/html"
)

// MaxSrcdocDepth bounds active embedded documents to 32 srcdoc crossings from
// the root (depth zero). Templates are inert; sandboxes disable execution but
// retain live resource edges. Active inline evidence beyond this bound fails.
const MaxSrcdocDepth = 32

type ExecutableSourceKind uint8

const (
	unknownExecutableSource ExecutableSourceKind = iota
	ScriptSource
	EventHandlerSource
	JavascriptURLSource
	RefreshURLSource
	executableSourceKindCount
)

// ExecutableSourceKinds enumerates the kinds emitted by the classifier. New
// kinds belong before executableSourceKindCount and need accounting witnesses.
func ExecutableSourceKinds() []ExecutableSourceKind {
	kinds := make([]ExecutableSourceKind, 0, executableSourceKindCount-1)
	for kind := ScriptSource; kind < executableSourceKindCount; kind++ {
		kinds = append(kinds, kind)
	}
	return kinds
}

// ExecutableSource describes active execution without running it. Body retains
// raw script bytes after srcdoc decoding, or decoded attribute code without URL
// scheme and refresh syntax. Only exact script bodies can match trusted signatures.
type ExecutableSource struct {
	Kind                        ExecutableSourceKind
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
					if strings.HasPrefix(key, "on") && len(key) > 2 {
						observe(doc, ExecutableSource{Kind: EventHandlerSource, Body: []byte(value)})
					} else if url {
						code, _ := javascriptURLCode(value)
						observe(doc, ExecutableSource{Kind: JavascriptURLSource, Body: []byte(code)})
					}
				}
				if node.Data == "script" && ScriptExecutes(node.Namespace, attrs) {
					body := doc.sources.lookup(node, attrs, scriptText(node))
					_, async := attrs["async"]
					_, deferred := attrs["defer"]
					_, external := attrs["src"]
					if node.Namespace == "svg" {
						_, href := attrs["href"]
						_, xlink := attrs["xlink:href"]
						external = href || xlink
					}
					inline := !external
					module := scriptASCIILower(attrs["type"]) == "module"
					observe(doc, ExecutableSource{Kind: ScriptSource, Script: true, Inline: inline, Synchronous: !module && (inline || !async && !deferred), Body: body.body, ExactBody: body.exact})
				}
				if node.Data == "meta" && strings.EqualFold(strings.TrimSpace(attrs["http-equiv"]), "refresh") && refreshJavascriptURL(attrs["content"]) {
					code, _ := refreshJavascriptURLCode(attrs["content"])
					observe(doc, ExecutableSource{Kind: RefreshURLSource, Body: []byte(code)})
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
