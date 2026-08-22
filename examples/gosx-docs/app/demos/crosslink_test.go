package docs

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func TestLiveDemoForGuideMapsDocumentedProofs(t *testing.T) {
	tests := []struct {
		guide    string
		wantSlug string
	}{
		{"/docs/compiler", "playground"},
		{"/docs/components", "playground"},
		{"/docs/islands", "playground"},
		{"/docs/hubs", "collab"},
		{"/docs/forms", "cms"},
		{"/docs/scene3d", "scene3d"},
		{"/docs/debugging-scene3d", "scene3d-bench"},
	}
	for _, test := range tests {
		demo, ok := LiveDemoForGuide(test.guide)
		if !ok || demo.Slug != test.wantSlug {
			t.Fatalf("LiveDemoForGuide(%q) = (%#v, %v), want demo %q", test.guide, demo, ok, test.wantSlug)
		}
	}

	for _, guide := range []string{"", "/docs", "/docs/signals", "/docs/nope", "/demos/playground"} {
		if demo, ok := LiveDemoForGuide(guide); ok {
			t.Fatalf("LiveDemoForGuide(%q) = %q, want no mapping", guide, demo.Slug)
		}
	}
}

func TestRelatedGuidesMirrorDocCatalogPairings(t *testing.T) {
	catalogPosition := map[string]int{}
	position := 0
	paired := map[string]string{}
	for _, section := range docsapp.DocsCatalog() {
		for _, entry := range section.Entries {
			catalogPosition[entry.Href] = position
			position++
			if entry.Demo == "" {
				continue
			}
			if _, ok := FindDemo(entry.Demo); !ok {
				t.Fatalf("guide %q points at unknown demo %q", entry.Href, entry.Demo)
			}
			paired[entry.Href] = entry.Demo
		}
	}
	if len(paired) == 0 {
		t.Fatal("no docs-to-demo pairings are configured")
	}

	mirrored := map[string]bool{}
	for _, demo := range Demos() {
		guides := RelatedGuides(demo.Slug)
		lastPosition := -1
		for _, guide := range guides {
			if guide.Demo != demo.Slug {
				t.Fatalf("RelatedGuides(%q) returned %q mapped to %q", demo.Slug, guide.Href, guide.Demo)
			}
			if catalogPosition[guide.Href] <= lastPosition {
				t.Fatalf("RelatedGuides(%q) is not in stable catalog order: %q after position %d", demo.Slug, guide.Href, lastPosition)
			}
			lastPosition = catalogPosition[guide.Href]

			back, ok := LiveDemoForGuide(guide.Href)
			if !ok || back.Slug != demo.Slug {
				t.Fatalf("pairing for %q is not symmetric: back = (%q, %v)", guide.Href, back.Slug, ok)
			}
			if mirrored[guide.Href] {
				t.Fatalf("guide %q is paired with more than one demo", guide.Href)
			}
			mirrored[guide.Href] = true
		}
	}
	if len(mirrored) != len(paired) {
		t.Fatalf("mirrored guides = %d, want %d pairings from the docs catalog", len(mirrored), len(paired))
	}
	if RelatedGuides("beacon") != nil {
		t.Fatal("RelatedGuides returned links for a demo with no documented pairing")
	}

	var playgroundHrefs []string
	for _, guide := range RelatedGuides("playground") {
		playgroundHrefs = append(playgroundHrefs, guide.Href)
	}
	want := []string{"/docs/components", "/docs/compiler", "/docs/islands"}
	if len(playgroundHrefs) != len(want) {
		t.Fatalf("playground guides = %#v, want %#v", playgroundHrefs, want)
	}
	for i, href := range want {
		if playgroundHrefs[i] != href {
			t.Fatalf("playground guides[%d] = %q, want %q", i, playgroundHrefs[i], href)
		}
	}
}

// TestDemosIndexRendersRelatedGuideLinks proves the user-facing half of the
// cross-navigation: every catalog pairing renders as an anchor on the demos
// index, and demos without a pairing render none.
func TestDemosIndexRendersRelatedGuideLinks(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	router := route.NewRouter()
	if err := router.AddDir(filepath.Dir(filepath.Dir(thisFile)), route.FileRoutesOptions{}); err != nil {
		t.Fatalf("add app routes: %v", err)
	}
	response := httptest.NewRecorder()
	router.Build().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/demos", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /demos status = %d", response.Code)
	}
	body := response.Body.String()

	featureLinks := strings.Count(body, `class="demo-feature__guide"`)
	rowLinks := strings.Count(body, `class="demo-row__guide"`)
	// Feature card: scene3d-bench -> Debugging Scene3D. Rows: playground ->
	// compiler/components/islands, collab -> hubs, cms -> forms,
	// scene3d -> 3D engine. Demos without a pairing render no guide link.
	if featureLinks != 1 || rowLinks != 6 {
		t.Fatalf("guide links on /demos: feature=%d row=%d, want 1 and 6", featureLinks, rowLinks)
	}
	for _, want := range []string{
		`href="/docs/compiler"`, `href="/docs/components"`, `href="/docs/islands"`,
		`href="/docs/hubs"`, `href="/docs/forms"`, `href="/docs/scene3d"`,
		`href="/docs/debugging-scene3d"`,
		"Compiler guide", "Components guide", "Islands guide",
		"Hubs &amp; CRDT guide", "Forms &amp; actions guide",
		"3D engine guide", "Debugging Scene3D guide",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered /demos is missing %q", want)
		}
	}
}
