package content

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/gosx"
)

// Collection describes a directory of content documents.
type Collection struct {
	Name       string
	Dir        string
	Extensions []string
}

// Document is a parsed content file with frontmatter separated from the body.
type Document struct {
	Collection  string
	Path        string
	SourcePath  string
	Slug        string
	Ext         string
	Frontmatter map[string]string
	Body        string
}

// Renderer renders a content document. mdpp or another Markdown/MDX renderer
// can implement this interface without becoming a core GoSX dependency.
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
	library := Library{
		docs:   make(map[string][]Document, len(collections)),
		bySlug: make(map[string]map[string]Document, len(collections)),
	}
	for _, collection := range collections {
		docs, err := LoadCollection(root, collection)
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
		doc := ParseDocument(name, filepath.ToSlash(rel), string(source))
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
	relPath = strings.TrimPrefix(filepath.ToSlash(relPath), "/")
	frontmatter, body := splitFrontmatter(source)
	ext := strings.ToLower(filepath.Ext(relPath))
	slug := strings.Trim(strings.TrimSpace(frontmatter["slug"]), "/")
	if slug == "" {
		slug = strings.TrimSuffix(relPath, ext)
	}
	return Document{
		Collection:  normalizeCollectionName(collection),
		Path:        relPath,
		Slug:        filepath.ToSlash(slug),
		Ext:         ext,
		Frontmatter: frontmatter,
		Body:        body,
	}
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

// Render renders a document using the provided renderer.
func (d Document) Render(renderer Renderer) (gosx.Node, error) {
	if renderer == nil {
		return gosx.Text(d.Body), nil
	}
	return renderer.Render(d)
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

func splitFrontmatter(source string) (map[string]string, string) {
	frontmatter := map[string]string{}
	trimmed := strings.TrimPrefix(source, "\ufeff")
	if !strings.HasPrefix(trimmed, "---\n") && !strings.HasPrefix(trimmed, "---\r\n") {
		return frontmatter, source
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
		return frontmatter, source
	}
	for _, line := range lines[1:end] {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || strings.HasPrefix(key, "#") {
			continue
		}
		frontmatter[key] = trimFrontmatterValue(value)
	}
	return frontmatter, strings.Join(lines[end+1:], "")
}

func trimFrontmatterValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
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
