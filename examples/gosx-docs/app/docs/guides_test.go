package docs

import (
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func guideRouter(t *testing.T) http.Handler {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	router := route.NewRouter()
	if err := router.AddDir(filepath.Dir(filepath.Dir(testFile)), route.FileRoutesOptions{}); err != nil {
		t.Fatalf("add docs app routes: %v", err)
	}
	return router.Build()
}

// Every catalog entry that sources a docs/*.md file must render through the
// guide route, so a new Markdown guide cannot be listed and then 404.
func TestMarkdownGuidesRenderFromTheirSource(t *testing.T) {
	handler := guideRouter(t)
	count := 0
	for _, section := range docsapp.DocsCatalog() {
		for _, entry := range section.Entries {
			if !strings.HasPrefix(entry.Source, "docs/") || !strings.HasSuffix(entry.Source, ".md") {
				continue
			}
			count++
			request := httptest.NewRequest(http.MethodGet, entry.Href, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d", entry.Href, response.Code)
			}
			body := response.Body.String()
			wants := []string{`class="docs-guide-body"`, html.EscapeString(entry.Title)}
			if source, err := guideContent.ReadFile("guides/content/" + strings.TrimPrefix(entry.Source, "docs/")); err != nil {
				t.Fatalf("read %s: %v", entry.Source, err)
			} else if strings.Contains("\n"+string(source), "\n## ") {
				wants = append(wants, "<h2 id=")
			}
			for _, want := range wants {
				if !strings.Contains(body, want) {
					t.Errorf("GET %s missing %q", entry.Href, want)
				}
			}
			if strings.Contains(body, "<h1>") && strings.Count(body, "<h1") > 1 {
				t.Errorf("GET %s renders a second h1; the shell supplies it", entry.Href)
			}
		}
	}
	if count < 15 {
		t.Fatalf("Markdown-sourced guides = %d, want the game, simulation and Scene3D guides", count)
	}
}

func TestUnknownGuideSlugIs404(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/docs/guides/not-a-guide", nil)
	response := httptest.NewRecorder()
	guideRouter(t).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}

func TestGuideLinksPointAtTheSiteOrTheRepository(t *testing.T) {
	for href, want := range map[string]string{
		"#section":                   "#section",
		"https://example.com/x":      "https://example.com/x",
		"netplay.md":                 "/docs/guides/netplay",
		"rooms.md#seats":             "/docs/guides/rooms#seats",
		"docs-site-operations.md":    guideBlobBase() + "docs/docs-site-operations.md",
		"../game/replay/README.md":   guideBlobBase() + "game/replay/README.md",
		"../examples/gosx-docs/app/": guideBlobBase() + "examples/gosx-docs/app",
	} {
		if got := guideLink(href); got != want {
			t.Errorf("guideLink(%q) = %q, want %q", href, got, want)
		}
	}
}

func TestGuideCopiesMatchDocs(t *testing.T) {
	_, testFile, _, _ := runtime.Caller(0)
	docsDir := filepath.Join(filepath.Dir(testFile), "..", "..", "..", "..", "..", "docs")
	want := map[string]bool{}
	for _, name := range docsapp.GuideSources() {
		want[name] = true
		source, err := os.ReadFile(filepath.Join(docsDir, name))
		if err != nil {
			t.Fatal(err)
		}
		copied, err := guideContent.ReadFile("guides/content/" + name)
		if err != nil {
			t.Fatalf("no copy of docs/%s; run go generate ./examples/gosx-docs/app/docs", name)
		}
		if string(copied) != string(source) {
			t.Errorf("guides/content/%s differs from docs/%s; run go generate ./examples/gosx-docs/app/docs", name, name)
		}
	}
	entries, err := guideContent.ReadDir("guides/content")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !want[e.Name()] {
			t.Errorf("guides/content/%s is not a catalog guide; run go generate ./examples/gosx-docs/app/docs", e.Name())
		}
	}
}

func TestGuideAnchorFollowsGitHubRules(t *testing.T) {
	for in, want := range map[string]string{
		"Terrain & scatter":      "terrain--scatter",
		"A & B":                  "a--b",
		"1. Describe the mode":   "1-describe-the-mode",
		"What's `Mode.Over`?":    "whats-modeover",
		"snake_case stays":       "snake_case-stays",
		"Ünicode Heading":        "ünicode-heading",
		"already-hyphenated one": "already-hyphenated-one",
	} {
		if got := guideAnchor(in); got != want {
			t.Errorf("guideAnchor(%q) = %q, want %q", in, got, want)
		}
	}
}
