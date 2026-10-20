package wire

import (
	"encoding/json"
	"strings"

	"golang.org/x/net/html"
)

// loadManifest in bootstrap-src/10-runtime-scene-utils.ts uses getElementById,
// then JSON.parse(el.textContent). Execution type and src do not select it.
// Each document (including srcdoc) has its own first candidate. Template
// contents are a separate DocumentFragment, not descendants in that document.
func documentManifestElement(root *html.Node) (*html.Node, error) {
	type pending struct {
		node  *html.Node
		depth int
	}
	stack := []pending{{root, 0}}
	for len(stack) > 0 {
		entry := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if entry.depth > maxReferenceDepth {
			return nil, referenceLimit("html-depth", maxReferenceDepth)
		}
		n := entry.node
		if n.Type == html.ElementNode && attr(n, "id") == "gosx-manifest" {
			return n, nil
		}
		if n.Type == html.ElementNode && n.Namespace == "" && n.Data == "template" {
			continue // The main walker records dropTemplateContent.
		}
		for child := n.LastChild; child != nil; child = child.PrevSibling {
			stack = append(stack, pending{child, entry.depth + 1})
		}
	}
	return nil, nil
}

// Unlike textOf (direct script/style text), DOM textContent concatenates all
// descendant text nodes, excluding comments and detached template contents.
func manifestElementTextContent(root *html.Node) (string, error) {
	type pending struct {
		node  *html.Node
		depth int
	}
	stack := []pending{{root, 0}}
	var text strings.Builder
	for len(stack) > 0 {
		entry := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if entry.depth > maxReferenceDepth {
			return "", referenceLimit("html-depth", maxReferenceDepth)
		}
		n := entry.node
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
		}
		if n.Type == html.ElementNode && n.Namespace == "" && n.Data == "template" {
			continue // The main walker records dropTemplateContent.
		}
		for child := n.LastChild; child != nil; child = child.PrevSibling {
			stack = append(stack, pending{child, entry.depth + 1})
		}
	}
	return text.String(), nil
}

func scanDocumentManifestReferences(root *html.Node, out *referenceScanner) error {
	element, err := documentManifestElement(root)
	if err != nil {
		return err
	}
	if element == nil {
		return nil // No manifest input; all nodes still reach the main walker.
	}
	if out.scriptsBlocked {
		out.drop(dropSandboxedExecutable)
		return nil
	}
	raw, err := manifestElementTextContent(element)
	if err != nil {
		return err
	}
	if !json.Valid([]byte(raw)) {
		// The loader catches parse failures and does not try a later ID.
		out.drop(dropInvalidManifest)
		return nil
	}
	out.drop(dropNestedScan)
	if err := scanHydrationReferences(raw, out); err != nil {
		// Valid JSON outside the supported producer schema is unresolved.
		// Keep scanning this element's executable attributes/body and siblings.
		out.drop(dropUnresolved)
	}
	return nil
}
