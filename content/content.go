package content

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/mdpp"
)

// Collection describes a directory of content documents.
type Collection struct {
	Name       string
	Dir        string
	Extensions []string
}

// Document is a parsed content file with frontmatter separated from the body.
type Document struct {
	Collection    string
	Path          string
	SourcePath    string
	Slug          string
	Ext           string
	Frontmatter   map[string]string
	Metadata      map[string]any
	Body          string
	Source        string
	Parsed        *mdpp.Document
	Diagnostics   []mdpp.Diagnostic
	RenderOptions mdpp.RenderOptions
}

// Renderer renders a content document. Custom renderers can wrap or replace
// the canonical mdpp renderer for specialized output.
type Renderer interface {
	Render(Document) (gosx.Node, error)
}

// RendererFunc adapts a function into a Renderer.
type RendererFunc func(Document) (gosx.Node, error)

// Render renders a document.
func (fn RendererFunc) Render(doc Document) (gosx.Node, error) {
	if fn == nil {
		return gosx.Text(""), nil
	}
	return fn(doc)
}

// Library is an indexed set of content collections.
type Library struct {
	docs   map[string][]Document
	bySlug map[string]map[string]Document
}

// Load loads one or more content collections rooted at root.
func Load(root string, collections ...Collection) (Library, error) {
	return LoadWithOptions(root, LoadOptions{}, collections...)
}

// LoadOptions configures content loading and mdpp rendering.
type LoadOptions struct {
	RenderOptions mdpp.RenderOptions
}

// MDPPOptions aliases mdpp.RenderOptions for content-local call sites.
type MDPPOptions = mdpp.RenderOptions

// LoadWithOptions loads one or more content collections rooted at root.
func LoadWithOptions(root string, opts LoadOptions, collections ...Collection) (Library, error) {
	library := Library{
		docs:   make(map[string][]Document, len(collections)),
		bySlug: make(map[string]map[string]Document, len(collections)),
	}
	for _, collection := range collections {
		docs, err := LoadCollectionWithOptions(root, collection, opts)
		if err != nil {
			return Library{}, err
		}
		name := collectionName(collection)
		library.docs[name] = docs
		slugIndex := make(map[string]Document, len(docs))
		for _, doc := range docs {
			slugIndex[doc.Slug] = doc
		}
		library.bySlug[name] = slugIndex
	}
	return library, nil
}

// LoadCollection loads all supported documents in a single collection.
func LoadCollection(root string, collection Collection) ([]Document, error) {
	return LoadCollectionWithOptions(root, collection, LoadOptions{})
}

// LoadCollectionWithOptions loads all supported documents in a single
// collection with explicit mdpp render options.
func LoadCollectionWithOptions(root string, collection Collection, opts LoadOptions) ([]Document, error) {
	name := collectionName(collection)
	if name == "" {
		return nil, fmt.Errorf("content collection name is required")
	}
	dir := collectionDir(root, collection.Dir)
	extensions := extensionSet(collection.Extensions)
	docs := []Document{}
	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := extensions[ext]; !ok {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		doc, err := ParseDocumentWithOptions(name, filepath.ToSlash(rel), string(source), opts)
		if err != nil {
			return err
		}
		doc.SourcePath = path
		docs = append(docs, doc)
		return nil
	}); err != nil {
		return nil, err
	}
	sort.SliceStable(docs, func(i, j int) bool {
		if docs[i].Slug == docs[j].Slug {
			return docs[i].Path < docs[j].Path
		}
		return docs[i].Slug < docs[j].Slug
	})
	return docs, nil
}

// ParseDocument parses a content document source.
func ParseDocument(collection, relPath, source string) Document {
	doc, _ := ParseDocumentWithOptions(collection, relPath, source, LoadOptions{})
	return doc
}

// ParseDocumentWithOptions parses a content document source through mdpp.
func ParseDocumentWithOptions(collection, relPath, source string, opts LoadOptions) (Document, error) {
	relPath = strings.TrimPrefix(filepath.ToSlash(relPath), "/")
	parsed, err := mdpp.Parse([]byte(source))
	if err != nil {
		return Document{}, fmt.Errorf("parse %s with mdpp: %w", relPath, err)
	}
	metadata := cloneMetadata(parsed.Frontmatter())
	frontmatter := frontmatterStrings(metadata)
	ext := strings.ToLower(filepath.Ext(relPath))
	slug := strings.Trim(strings.TrimSpace(metadataString(metadata, "slug")), "/")
	if slug == "" {
		slug = strings.TrimSuffix(relPath, ext)
	}
	return Document{
		Collection:    normalizeCollectionName(collection),
		Path:          relPath,
		Slug:          filepath.ToSlash(slug),
		Ext:           ext,
		Frontmatter:   frontmatter,
		Metadata:      metadata,
		Body:          stripFrontmatter(source),
		Source:        source,
		Parsed:        parsed,
		Diagnostics:   parsed.Diagnostics(),
		RenderOptions: opts.RenderOptions,
	}, nil
}

// MDPPRenderer renders documents with mdpp.
type MDPPRenderer struct {
	Options mdpp.RenderOptions
}

// NewMDPPRenderer creates a renderer backed by mdpp.
func NewMDPPRenderer(opts mdpp.RenderOptions) MDPPRenderer {
	return MDPPRenderer{Options: opts}
}

// Render renders a document with mdpp and returns raw HTML.
func (r MDPPRenderer) Render(doc Document) (gosx.Node, error) {
	parsed := doc.Parsed
	if parsed == nil {
		var err error
		parsed, err = mdpp.Parse([]byte(doc.Source))
		if err != nil {
			return gosx.Text(""), err
		}
	}
	html, err := mdpp.Render(parsed, r.Options)
	if err != nil {
		return gosx.Text(""), err
	}
	return gosx.RawHTML(string(html)), nil
}

// HTML renders the document with mdpp and returns HTML.
func (d Document) HTML(opts mdpp.RenderOptions) (string, error) {
	node, err := MDPPRenderer{Options: opts}.Render(d)
	if err != nil {
		return "", err
	}
	return gosx.RenderHTML(node), nil
}

// Render renders a document using the provided renderer. A nil renderer uses
// mdpp, making Markdown++ the canonical content-source renderer.
func (d Document) Render(renderer Renderer) (gosx.Node, error) {
	if renderer == nil {
		return MDPPRenderer{Options: d.RenderOptions}.Render(d)
	}
	return renderer.Render(d)
}

func cloneMetadata(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func frontmatterStrings(values map[string]any) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = metadataValueString(value)
	}
	return out
}

func metadataString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	return metadataValueString(values[key])
}

func metadataValueString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func stripFrontmatter(source string) string {
	trimmed := strings.TrimPrefix(source, "\ufeff")
	if !strings.HasPrefix(trimmed, "---\n") && !strings.HasPrefix(trimmed, "---\r\n") {
		return source
	}
	lines := strings.SplitAfter(trimmed, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return source
	}
	return strings.Join(lines[end+1:], "")
}

// Collection returns the documents in a named collection.
func (l Library) Collection(name string) []Document {
	docs := l.docs[normalizeCollectionName(name)]
	return append([]Document(nil), docs...)
}

// BySlug returns a document from a named collection by slug.
func (l Library) BySlug(collection, slug string) (Document, bool) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" {
		return Document{}, false
	}
	docs := l.bySlug[normalizeCollectionName(collection)]
	if docs == nil {
		return Document{}, false
	}
	doc, ok := docs[filepath.ToSlash(slug)]
	return doc, ok
}

func collectionDir(root, dir string) string {
	root = strings.TrimSpace(root)
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = "."
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	if root == "" {
		root = "."
	}
	return filepath.Join(root, dir)
}

func extensionSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		values = []string{".md", ".mdx", ".mdpp"}
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if !strings.HasPrefix(value, ".") {
			value = "." + value
		}
		out[value] = struct{}{}
	}
	return out
}

func normalizeCollectionName(name string) string {
	return strings.Trim(strings.TrimSpace(name), "/")
}

func collectionName(collection Collection) string {
	if name := normalizeCollectionName(collection.Name); name != "" {
		return name
	}
	dir := strings.Trim(filepath.ToSlash(strings.TrimSpace(collection.Dir)), "/")
	if dir == "" || dir == "." {
		return ""
	}
	return normalizeCollectionName(filepath.Base(dir))
}
