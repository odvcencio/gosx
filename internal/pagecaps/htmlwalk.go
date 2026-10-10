package pagecaps

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// HTMLState is execution evidence for a node in one parsed document. Script
// selects script elements; Executable also includes handlers and script URLs.
type HTMLState struct {
	Attributes map[string]string
	Inert      bool
	Script     bool
	Executable bool
}

// WalkHTML is the common execution walk for capabilities and measurement.
// The HTML5 tree builder owns namespaces and scripting-enabled noscript/raw
// text semantics. Only HTML templates make their subtree inert. Encoded srcdoc
// is an attribute, not a child tree; document consumers parse it separately and
// use this same walk for that document with its embedding restrictions.
func WalkHTML(root *html.Node, visit func(*html.Node, HTMLState) error) error {
	var walk func(*html.Node, bool) error
	walk = func(node *html.Node, inert bool) error {
		state := HTMLState{Inert: inert}
		if node.Type == html.ElementNode {
			state.Inert = inert || node.Namespace == "" && node.DataAtom == atom.Template
			state.Attributes = map[string]string{}
			for _, attr := range node.Attr {
				key := scriptASCIILower(attr.Key)
				if attr.Namespace != "" {
					key = attr.Namespace + ":" + key
				}
				if _, exists := state.Attributes[key]; !exists {
					state.Attributes[key] = attr.Val
				}
			}
			if !state.Inert {
				state.Script = node.DataAtom == atom.Script && ScriptExecutes(node.Namespace, state.Attributes)
				state.Executable = state.Script
				for key, value := range state.Attributes {
					if strings.HasPrefix(key, "on") && len(key) > 2 || strings.HasPrefix(scriptASCIILower(strings.TrimSpace(value)), "javascript:") {
						state.Executable = true
					}
				}
			}
		}
		if err := visit(node, state); err != nil {
			return err
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child, state.Inert); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root, false)
}
