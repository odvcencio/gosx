package docs

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

func TestDocsCatalogIsCompleteUniqueAndSourceLinked(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", "..", ".."))
	catalog := DocsCatalog()
	if len(catalog) < 4 {
		t.Fatalf("sections = %d, want at least 4", len(catalog))
	}
	seenSections := map[string]bool{}
	seenRoutes := map[string]bool{}
	seenSources := map[string]bool{}
	entryCount := 0
	for _, section := range catalog {
		if section.ID == "" || section.Title == "" || section.Description == "" {
			t.Fatalf("incomplete section: %#v", section)
		}
		if seenSections[section.ID] {
			t.Fatalf("duplicate section ID %q", section.ID)
		}
		seenSections[section.ID] = true
		for _, entry := range section.Entries {
			entryCount++
			if entry.Title == "" || entry.Description == "" || entry.Source == "" || len(entry.Keywords) == 0 {
				t.Fatalf("incomplete entry: %#v", entry)
			}
			if entry.Section != section.ID {
				t.Fatalf("%s section = %q, want %q", entry.Href, entry.Section, section.ID)
			}
			if !strings.HasPrefix(entry.Href, "/docs/") {
				t.Fatalf("catalog route %q is outside /docs", entry.Href)
			}
			if seenRoutes[entry.Href] {
				t.Fatalf("duplicate route %q", entry.Href)
			}
			if seenSources[entry.Source] {
				t.Fatalf("duplicate source %q", entry.Source)
			}
			if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(entry.Source))); err != nil {
				t.Fatalf("catalog source %q is not readable: %v", entry.Source, err)
			}
			seenRoutes[entry.Href] = true
			seenSources[entry.Source] = true
		}
	}
	if entryCount < 20 {
		t.Fatalf("catalog entries = %d, want full documentation set", entryCount)
	}
	for _, routePath := range []string{"/docs/typed-live", "/docs/site", "/docs/components", "/docs/deployment"} {
		if !seenRoutes[routePath] {
			t.Fatalf("catalog missing %q", routePath)
		}
	}

	routes := DocsCatalogRoutes()
	if len(routes) != entryCount+1 || routes[0] != "/docs" {
		t.Fatalf("catalog routes = %#v", routes)
	}
	for i, routePath := range routes[1:] {
		if !seenRoutes[routePath] {
			t.Fatalf("catalog route %d %q has no entry", i+1, routePath)
		}
	}
}

func TestDocsCatalogReturnsIsolatedCopy(t *testing.T) {
	first := DocsCatalog()
	first[0].Title = "mutated"
	first[0].Entries[0].Title = "mutated"
	first[0].Entries[0].Keywords[0] = "mutated"

	second := DocsCatalog()
	if second[0].Title == "mutated" || second[0].Entries[0].Title == "mutated" || second[0].Entries[0].Keywords[0] == "mutated" {
		t.Fatal("DocsCatalog returned mutable shared state")
	}
}

func TestSearchDocsCatalogRanksAndRequiresEveryTerm(t *testing.T) {
	results := SearchDocsCatalog("strict typed props")
	if len(results) == 0 || results[0].Entry.Href != "/docs/typed-live" {
		t.Fatalf("strict search results = %#v", results)
	}

	results = SearchDocsCatalog("webgpu fallback")
	if len(results) == 0 {
		t.Fatal("WebGPU fallback search returned no results")
	}
	for _, result := range results {
		if result.Score <= 0 {
			t.Fatalf("non-positive search score: %#v", result)
		}
	}

	if got := SearchDocsCatalog("webgpu definitely-not-a-guide"); len(got) != 0 {
		t.Fatalf("AND search returned %#v", got)
	}
	if got := SearchDocsCatalog("  "); got != nil {
		t.Fatalf("blank search = %#v, want nil", got)
	}
}

func TestDocsNavigationMarksOnlyCurrentRoute(t *testing.T) {
	navigation := DocsNavigation("/docs/forms")
	current := 0
	for _, section := range navigation {
		entries, ok := section["entries"].([]map[string]any)
		if !ok {
			t.Fatalf("entries type = %T", section["entries"])
		}
		for _, entry := range entries {
			className, _ := entry["className"].(string)
			if strings.Contains(className, "is-current") {
				current++
				if entry["href"] != "/docs/forms" {
					t.Fatalf("current entry = %#v", entry)
				}
				if entry["ariaCurrent"] != "page" {
					t.Fatalf("current entry lacks semantic marker: %#v", entry)
				}
			} else if entry["ariaCurrent"] != nil {
				t.Fatalf("non-current entry has semantic marker: %#v", entry)
			}
		}
	}
	if current != 1 {
		t.Fatalf("current entries = %d, want 1", current)
	}
	if got := DocsIndexClassName("/docs"); !strings.Contains(got, "is-current") {
		t.Fatalf("index class = %q", got)
	}
	if got := DocsIndexAriaCurrent("/docs"); got != "page" {
		t.Fatalf("index aria-current = %#v", got)
	}
	if got := DocsIndexAriaCurrent("/docs/forms"); got != nil {
		t.Fatalf("non-index aria-current = %#v", got)
	}
}

func TestDocsPageLinksCoverLearningPathAndSource(t *testing.T) {
	index := DocsPageLinks("/docs")
	if index["previous"] != nil || index["next"] == nil {
		t.Fatalf("index learning links = %#v", index)
	}
	components := DocsPageLinks("/docs/components")
	if components["previous"] == nil || components["next"] == nil {
		t.Fatalf("components learning links = %#v", components)
	}
	if source, _ := components["sourceURL"].(string); !strings.HasSuffix(source, "/examples/gosx-docs/app/docs/components/page.gsx") {
		t.Fatalf("components source URL = %q", source)
	}
	missing := DocsPageLinks("/docs/not-real")
	if len(missing) != 0 {
		t.Fatalf("unknown page links = %#v", missing)
	}
}

func TestDefaultDocsBindingsExposeCatalogAndBuildIdentity(t *testing.T) {
	request := httptest.NewRequest("GET", "https://gosx.m31labs.dev/docs/forms", nil)
	bindings := defaultDocsBindings(&route.RouteContext{Request: request}, route.FilePage{RoutePath: "/docs/forms"})
	if _, ok := bindings.Values["docsNavigation"].([]map[string]any); !ok {
		t.Fatalf("docs navigation binding = %T", bindings.Values["docsNavigation"])
	}
	if _, ok := bindings.Values["site"].(map[string]string); !ok {
		t.Fatalf("site binding = %T", bindings.Values["site"])
	}
}
