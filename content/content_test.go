package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestLoadCollectionParsesFrontmatterAndIndexesSlugs(t *testing.T) {
	root := t.TempDir()
	posts := filepath.Join(root, "content", "posts")
	if err := os.MkdirAll(filepath.Join(posts, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(posts, "hello.mdx"), []byte("---\ntitle: Hello\nslug: hello-world\n---\n# Hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(posts, "nested", "second.md"), []byte("---\ntitle: Second\n---\n# Second\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(posts, "ignored.txt"), []byte("ignore"), 0644); err != nil {
		t.Fatal(err)
	}

	library, err := Load(root, Collection{Name: "posts", Dir: "content/posts"})
	if err != nil {
		t.Fatal(err)
	}
	docs := library.Collection("posts")
	if len(docs) != 2 {
		t.Fatalf("expected two docs, got %#v", docs)
	}
	doc, ok := library.BySlug("posts", "hello-world")
	if !ok {
		t.Fatal("expected hello-world slug")
	}
	if doc.Frontmatter["title"] != "Hello" || strings.TrimSpace(doc.Body) != "# Hello" {
		t.Fatalf("unexpected parsed document %#v", doc)
	}
	if _, ok := library.BySlug("posts", "nested/second"); !ok {
		t.Fatal("expected nested slug")
	}
}

func TestDocumentRenderUsesRendererHook(t *testing.T) {
	doc := ParseDocument("docs", "intro.md", "# Intro")
	node, err := doc.Render(RendererFunc(func(doc Document) (gosx.Node, error) {
		return gosx.El("article", gosx.Text(doc.Body)), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	html := gosx.RenderHTML(node)
	if !strings.Contains(html, "<article># Intro</article>") {
		t.Fatalf("unexpected rendered content %q", html)
	}
}

func TestLoadInfersCollectionNameFromDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "content", "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "content", "notes", "one.md"), []byte("One"), 0644); err != nil {
		t.Fatal(err)
	}

	library, err := Load(root, Collection{Dir: "content/notes"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := library.BySlug("notes", "one"); !ok {
		t.Fatal("expected inferred notes collection")
	}
}
