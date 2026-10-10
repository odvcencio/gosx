package pagecaps

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// Document is one browsing document. Srcdoc bodies are entity-decoded by the
// HTML tree builder. URL identifies the containing fetched body; Key separates
// inline occurrences while allowing repeated fetched bodies to share evidence.
// Execution permission is inherited and cannot be restored by a child sandbox.
type Document struct {
	Root           *html.Node
	Body           []byte
	URL, Key       string
	BaseURL        string
	Depth          int
	ScriptsAllowed bool
	Inline         bool
	Frames         []*DocumentFrame
	sources        *scriptSources
	embeddings     map[*html.Node]*DocumentFrame
}

// DocumentFrame keeps the embedding relationship even when the source cannot
// be resolved. A srcdoc attribute always replaces Source, including empty
// and sandboxed srcdoc. Templates never contribute frames.
type DocumentFrame struct {
	Element        *html.Node
	Source         string
	ScriptsAllowed bool
	Child          *Document
}

type DocumentTree struct {
	Root      *Document
	Documents []*Document
	Complete  bool
}

// DocumentLoader resolves a fetched frame against its containing document.
// A false found result keeps the tree incomplete. It never grants execution.
type DocumentLoader func(base, reference string) (body []byte, resolved string, found bool, err error)

// Keep walker membership explicit so the independent spec-backed embedding
// test table must cover every supported element.
var documentEmbeddingElements = map[string]bool{"iframe": true, "frame": true}

// ParseDocumentTree models live documents with a maximum of 32 embedding
// crossings and 4096 occurrences. An unresolved fetched tree is incomplete;
// an over-depth executable inline tree without a loader remains an input error.
// Without a loader, fetched frames are retained as edges for resource scanning.
func ParseDocumentTree(body []byte, documentURL string, load DocumentLoader) (*DocumentTree, error) {
	tree := &DocumentTree{Complete: true}
	var parse func([]byte, string, string, string, bool, bool, int, map[string]bool) (*Document, error)
	parse = func(data []byte, u, key, inheritedBase string, allowed, inline bool, depth int, path map[string]bool) (*Document, error) {
		if depth > MaxSrcdocDepth || len(tree.Documents) >= 4096 {
			if load == nil && allowed {
				return nil, errors.New("invalid capability HTML")
			}
			tree.Complete = false
			return nil, nil
		}
		if len(data) > 16<<20 || !utf8.Valid(data) {
			return nil, errors.New("invalid capability HTML")
		}
		root, err := html.Parse(bytes.NewReader(data))
		if err != nil {
			return nil, errors.New("invalid capability HTML")
		}
		doc := &Document{Root: root, Body: data, URL: u, Key: key, Depth: depth, ScriptsAllowed: allowed, Inline: inline, sources: readScriptSources(data), embeddings: map[*html.Node]*DocumentFrame{}}
		doc.BaseURL = u
		if inline {
			doc.BaseURL = inheritedBase
		}
		baseSeen := false
		_ = doc.Walk(func(n *html.Node, attrs map[string]string, _ int) error {
			if !baseSeen && n.Namespace == "" && n.Data == "base" {
				if href, ok := attrs["href"]; ok {
					baseSeen = true
					parent, err := url.Parse(doc.BaseURL)
					child, childErr := url.Parse(strings.Trim(href, " \t\r\n\f"))
					if err != nil || childErr != nil {
						doc.BaseURL = ""
					} else {
						doc.BaseURL = parent.ResolveReference(child).String()
					}
				}
			}
			return nil
		})
		tree.Documents = append(tree.Documents, doc)
		if err := doc.Walk(func(n *html.Node, attrs map[string]string, _ int) error {
			if n.Type != html.ElementNode || n.Namespace != "" || !documentEmbeddingElements[n.Data] {
				return nil
			}
			frame := &DocumentFrame{Element: n, ScriptsAllowed: allowed && (n.Data != "iframe" || scriptsAllowed(attrs))}
			ordinal := len(doc.Frames)
			doc.Frames = append(doc.Frames, frame)
			doc.embeddings[n] = frame
			if content, present := attrs["srcdoc"]; present && n.Data == "iframe" {
				child, err := parse([]byte(content), u, fmt.Sprintf("inline:%s/%d", key, ordinal), doc.BaseURL, frame.ScriptsAllowed, true, depth+1, path)
				if err != nil {
					return err
				}
				frame.Child = child
				return nil
			}
			frame.Source = strings.TrimSpace(attrs["src"])
			if frame.Source == "" || javascriptURL(frame.Source) {
				frame.Source = ""
				return nil
			}
			if load == nil {
				return nil
			}
			data, resolved, found, err := load(doc.BaseURL, frame.Source)
			if err != nil {
				return err
			}
			if !found || path[resolved] {
				tree.Complete = false
				return nil
			}
			next := make(map[string]bool, len(path)+1)
			for k, v := range path {
				next[k] = v
			}
			next[resolved] = true
			frame.Child, err = parse(data, resolved, "fetched:"+resolved, "", frame.ScriptsAllowed, false, depth+1, next)
			return err
		}); err != nil {
			return nil, err
		}
		return doc, nil
	}
	key := "fetched:" + documentURL
	if documentURL == "" {
		key = "document"
	}
	root, err := parse(body, documentURL, key, "", true, false, 0, map[string]bool{documentURL: true})
	if err != nil {
		return nil, err
	}
	tree.Root = root
	return tree, nil
}

// Walk visits this document's live DOM, excluding HTML template contents. All
// consumers use the HTML5 tree builder's namespaces and first-attribute rule.
// Other documents are explicit nodes rather than hidden recursive tag scans.
func (doc *Document) Walk(visit func(*html.Node, map[string]string, int) error) error {
	return WalkHTML(doc.Root, func(n *html.Node, state HTMLState) error {
		if state.Inert {
			return nil
		}
		return visit(n, state.Attributes, state.Depth)
	})
}

// Embedding retrieves the frame associated with a live element in this model.
func (doc *Document) Embedding(node *html.Node) *DocumentFrame { return doc.embeddings[node] }
