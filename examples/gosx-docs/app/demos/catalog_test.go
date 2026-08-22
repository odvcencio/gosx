package docs

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
)

func TestDemoCatalogContracts(t *testing.T) {
	if len(Demos()) != 12 {
		t.Fatalf("Demos() length = %d, want 12", len(Demos()))
	}
	seen := make(map[string]bool, len(Demos()))
	validStatus := map[string]bool{"featured": true, "live": true, "lab": true, "prototype": true}
	for _, demo := range Demos() {
		if demo.Slug == "" || seen[demo.Slug] {
			t.Fatalf("empty or duplicate demo slug %q", demo.Slug)
		}
		seen[demo.Slug] = true
		if demo.Title == "" || demo.Promise == "" || demo.Lesson == "" {
			t.Errorf("demo %q lacks a title, promise, or lesson", demo.Slug)
		}
		if !validStatus[demo.Status] {
			t.Errorf("demo %q has invalid status %q", demo.Slug, demo.Status)
		}
		if len(demo.Facets) == 0 || len(demo.Packages) == 0 || demo.SourcePath == "" {
			t.Errorf("demo %q lacks proof metadata", demo.Slug)
		}
		if demo.RenderMode == "" || demo.Limitations == "" {
			t.Errorf("demo %q lacks honest runtime metadata", demo.Slug)
		}
		if _, err := os.Stat(repoPath(t, demo.SourcePath)); err != nil {
			t.Errorf("demo %q source path %q: %v", demo.Slug, demo.SourcePath, err)
		}
	}
	// CMS earned "live" once block adding, live preview, and full-draft publish
	// became real; its limitations must keep documenting what is still missing.
	cms, ok := FindDemo("cms")
	if !ok || cms.Status != "live" {
		t.Error("CMS must be listed live now that block editing and live preview are real")
	}
	for _, required := range []string{"no persistence", "no reordering", "no block removal"} {
		if !strings.Contains(cms.Limitations, required) {
			t.Errorf("cms limitations missing %q: %s", required, cms.Limitations)
		}
	}
	checkers, ok := FindDemo("checkers")
	if !ok || checkers.Status != "live" {
		t.Fatal("checkers must be truthfully listed as a live two-seat match")
	}
	for _, required := range []string{"two-player", "no product network multiplayer", "persistence", "active CPU", "compiled Arbiter policy fallback", "Elio"} {
		if !strings.Contains(checkers.Limitations, required) {
			t.Errorf("checkers limitations missing %q: %s", required, checkers.Limitations)
		}
	}
	for _, required := range []string{"Selena", "Arbiter policy", "Elio adapter"} {
		if !contains(checkers.Facets, required) {
			t.Errorf("checkers facets missing %q", required)
		}
	}
}

func TestShowcaseDemosHaveStableTruthfulPromotion(t *testing.T) {
	want := []struct {
		slug   string
		status string
	}{
		{slug: "beacon", status: "featured"},
		{slug: "water", status: "featured"},
		{slug: "checkers", status: "live"},
		{slug: "scene3d-bench", status: "lab"},
	}
	got := ShowcaseDemos()
	if len(got) != len(want) {
		t.Fatalf("ShowcaseDemos() length = %d, want %d", len(got), len(want))
	}
	for i, demo := range got {
		if demo.Slug != want[i].slug || demo.Status != want[i].status {
			t.Errorf("ShowcaseDemos()[%d] = %q (%s), want %q (%s)", i, demo.Slug, demo.Status, want[i].slug, want[i].status)
		}
		if demo.ShowcaseRank != i+1 {
			t.Errorf("ShowcaseDemos()[%d].ShowcaseRank = %d, want %d", i, demo.ShowcaseRank, i+1)
		}
		if !strings.HasPrefix(demoSourceURL(demo.SourcePath), "https://github.com/odvcencio/gosx/blob/main/") {
			t.Errorf("showcase demo %q does not resolve to a source link", demo.Slug)
		}
	}
	if got[3].Status == "featured" {
		t.Error("the benchmark must remain labelled lab even when editorially promoted")
	}
	if len(AdditionalDemos())+len(got) != len(Demos()) {
		t.Error("showcase and additional catalog slices must partition Demos()")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestDemoShellUsesGoSXManagedInteractions(t *testing.T) {
	layout, err := os.ReadFile(repoPath(t, "examples/gosx-docs/app/demos/layout.gsx"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(layout)
	for _, required := range []string{
		`data-gosx-bind-source`,
		`data-gosx-toggle-target`,
		`data-gosx-disclosure-target`,
		`data-gosx-disclosure-close`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("demo layout missing GoSX-managed behavior %q", required)
		}
	}
	if strings.Contains(source, `<script`) {
		t.Error("demo layout must not ship bespoke script elements")
	}
}

func TestDemoLayoutRendersMobileDockWithCompleteCatalog(t *testing.T) {
	layoutPath := repoPath(t, "examples/gosx-docs/app/demos/layout.gsx")
	layout, err := route.FileLayout(layoutPath)
	if err != nil {
		t.Fatal(err)
	}

	ctx := &route.RouteContext{Request: httptest.NewRequest(http.MethodGet, "/demos/playground", nil)}
	rendered := gosx.RenderHTML(layout(ctx, gosx.El("main", gosx.Attrs(gosx.Attr("data-test-slot", "playground")))))
	if status := ctx.StatusCode(); status != 0 && status != http.StatusOK {
		t.Fatalf("layout render status = %d; body=%s", status, rendered)
	}

	doc, err := html.Parse(strings.NewReader(rendered))
	if err != nil {
		t.Fatalf("parse rendered layout: %v", err)
	}

	links := make(map[string]map[string]string, len(Demos()))
	var hasDock, hasMobileMenu, hasSlot bool
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			attrs := htmlAttributes(node)
			classes := strings.Fields(attrs["class"])
			switch {
			case node.Data == "nav" && attrs["id"] == "demo-dock":
				hasDock = attrs["aria-label"] == "Demos"
			case node.Data == "button" && contains(classes, "demos-topbar__menu"):
				hasMobileMenu = attrs["aria-controls"] == "demo-dock" &&
					attrs["data-gosx-toggle-target"] == ".demos-body" &&
					attrs["data-gosx-toggle-attribute"] == "data-dock-open"
			case node.Data == "a" && contains(classes, "demo-dock__link"):
				links[attrs["href"]] = attrs
			case node.Data == "main" && attrs["data-test-slot"] == "playground":
				hasSlot = true
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)

	if !hasDock || !hasMobileMenu {
		t.Fatalf("rendered demo shell is missing its accessible mobile dock controls: %s", rendered)
	}
	if !hasSlot {
		t.Fatalf("rendered demo layout dropped its page slot: %s", rendered)
	}
	if len(links) != len(Demos()) {
		t.Fatalf("rendered dock links = %d, want %d; body=%s", len(links), len(Demos()), rendered)
	}
	for _, demo := range Demos() {
		href := "/demos/" + demo.Slug
		attrs, ok := links[href]
		if !ok {
			t.Errorf("rendered dock missing %s", href)
			continue
		}
		if attrs["data-demo"] != demo.Slug || attrs["data-demo-title"] != demo.Title || attrs["data-demo-source-path"] != demo.SourcePath {
			t.Errorf("rendered dock metadata for %s = %#v", href, attrs)
		}
	}
}

func TestDemoLayoutDirectLoadsRenderCurrentProofMetadata(t *testing.T) {
	layoutPath := repoPath(t, "examples/gosx-docs/app/demos/layout.gsx")
	layout, err := route.FileLayout(layoutPath)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range Demos() {
		want := want
		t.Run(want.Slug, func(t *testing.T) {
			ctx := &route.RouteContext{Request: httptest.NewRequest(http.MethodGet, "/demos/"+want.Slug, nil)}
			rendered := gosx.RenderHTML(layout(ctx, gosx.El("main", gosx.Text(want.Title))))
			if status := ctx.StatusCode(); status != 0 && status != http.StatusOK {
				t.Fatalf("layout render status = %d; body=%s", status, rendered)
			}

			doc, parseErr := html.Parse(strings.NewReader(rendered))
			if parseErr != nil {
				t.Fatalf("parse rendered layout: %v", parseErr)
			}

			currentHrefs := []string{}
			currentManaged := []string{}
			facts := map[string]string{}
			var shellSlug, title, lesson, sourceHref, sourcePath string
			var visit func(*html.Node)
			visit = func(node *html.Node) {
				if node.Type == html.ElementNode {
					attrs := htmlAttributes(node)
					classes := strings.Fields(attrs["class"])
					switch {
					case contains(classes, "demos-shell"):
						shellSlug = attrs["data-demo-slug"]
					case node.Data == "a" && contains(classes, "demo-dock__link") && attrs["aria-current"] == "page":
						currentHrefs = append(currentHrefs, attrs["href"])
						currentManaged = append(currentManaged, attrs["data-gosx-aria-current-managed"])
					case node.Data == "h2" && attrs["id"] == "demo-details-title":
						title = normalizedNodeText(node)
					case node.Data == "p" && contains(classes, "demo-details__lesson"):
						lesson = normalizedNodeText(node)
					case node.Data == "dd" && attrs["data-gosx-bind-text"] != "":
						facts[attrs["data-gosx-bind-text"]] = normalizedNodeText(node)
					case node.Data == "a" && contains(classes, "demo-details__source"):
						sourceHref = attrs["href"]
					case node.Data == "code" && contains(classes, "demo-details__path"):
						sourcePath = normalizedNodeText(node)
					}
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					visit(child)
				}
			}
			visit(doc)

			if len(currentHrefs) != 1 || currentHrefs[0] != "/demos/"+want.Slug {
				t.Fatalf("server-rendered current links = %#v, want only /demos/%s", currentHrefs, want.Slug)
			}
			if len(currentManaged) != 1 || currentManaged[0] != "true" {
				t.Fatalf("server-rendered current link ownership = %#v, want managed current state", currentManaged)
			}
			if shellSlug != want.Slug {
				t.Errorf("server-rendered shell slug = %q, want %q", shellSlug, want.Slug)
			}
			if title != want.Title || lesson != want.Lesson {
				t.Errorf("server-rendered details = title %q, lesson %q", title, lesson)
			}
			wantFacts := map[string]string{
				"data-demo-facets":      demoValues(want.Facets),
				"data-demo-packages":    demoValues(want.Packages),
				"data-demo-render-mode": want.RenderMode,
				"data-demo-limitations": want.Limitations,
			}
			for binding, wantValue := range wantFacts {
				if facts[binding] != wantValue {
					t.Errorf("server-rendered %s = %q, want %q", binding, facts[binding], wantValue)
				}
			}
			if sourceHref != demoSourceURL(want.SourcePath) || sourcePath != want.SourcePath {
				t.Errorf("server-rendered source = (%q, %q), want (%q, %q)", sourceHref, sourcePath, demoSourceURL(want.SourcePath), want.SourcePath)
			}
		})
	}
}

func normalizedNodeText(node *html.Node) string {
	parts := []string{}
	var visit func(*html.Node)
	visit = func(current *html.Node) {
		if current.Type == html.TextNode {
			parts = append(parts, current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func htmlAttributes(node *html.Node) map[string]string {
	attrs := make(map[string]string, len(node.Attr))
	for _, attr := range node.Attr {
		attrs[attr.Key] = attr.Val
	}
	return attrs
}

func TestBespokeDemoScriptDebtDoesNotGrow(t *testing.T) {
	// These predate the no-escape-hatch invariant. Keep the list exact: a new
	// script fails immediately, and deleting one requires deleting its entry so
	// the debt can only move toward zero.
	legacy := map[string]bool{
		"checkers/page.gsx":      true,
		"cms/page.gsx":           true,
		"collab/page.gsx":        true,
		"fluid/page.gsx":         true,
		"livesim/page.gsx":       true,
		"playground/page.gsx":    true,
		"scene3d/page.gsx":       true,
		"scene3d-bench/page.gsx": true,
	}
	root := repoPath(t, "examples/gosx-docs/app/demos")
	found := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".gsx" {
			return err
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !strings.Contains(string(source), `<script`) {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		relative = filepath.ToSlash(relative)
		found[relative] = true
		if !legacy[relative] {
			t.Errorf("new bespoke demo script in %s; add the behavior to GoSX instead", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for relative := range legacy {
		if !found[relative] {
			t.Errorf("legacy script debt %s was removed; delete its exception too", relative)
		}
	}
}

func repoPath(t *testing.T, relative string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	return filepath.Join(root, filepath.FromSlash(relative))
}
