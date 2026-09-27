package docs

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestDemosIndexRendersCuratedGalleryWithoutMountingScenes(t *testing.T) {
	page := readDemoSource(t, "examples/gosx-docs/app/demos/page.gsx")
	if strings.Contains(page, "<Scene3D ") {
		t.Fatal("demos index must defer the Scene3D mount to its own route")
	}
	if strings.Contains(page, "<main") {
		t.Error("demos index must not nest a main landmark inside the application main")
	}
	for _, required := range []string{
		`<section class="demos-gallery" aria-labelledby="demos-gallery-title">`,
		`id="demos-gallery-title"`,
		`data.featured`,
		`data.groups`,
		`demo.PosterPath`,
		`demo.Backends`,
		`demo.SourcePaths`,
		`demoSourceURL(demo.SourcePath)`,
	} {
		if !strings.Contains(page, required) {
			t.Errorf("demos index missing rendered showreel contract %q", required)
		}
	}
	if strings.Contains(page, "<script") {
		t.Error("demos index must not add bespoke script behavior")
	}
	if got := strings.Count(page, `data-gosx-link="true"`); got != 8 {
		t.Errorf("demos index managed-navigation link declarations = %d, want 8", got)
	}
	if strings.Contains(page, `target="_blank" data-gosx-link="true"`) {
		t.Error("external source links must not be intercepted by managed navigation")
	}
}

func TestDemosIndexStylesHonorTokensResponsiveLayoutAndReducedMotion(t *testing.T) {
	css := readDemoSource(t, "examples/gosx-docs/app/demos/page.css")
	for _, required := range []string{
		`var(--font-display)`,
		`var(--color-accent)`,
		`var(--space-xl)`,
		`var(--duration-normal)`,
		`var(--radius-lg)`,
		`@media (max-width: 700px)`,
		`@media (prefers-reduced-motion: reduce)`,
		`.demo-card__poster`,
		`.demo-card__sources`,
	} {
		if !strings.Contains(css, required) {
			t.Errorf("demos index CSS missing %q", required)
		}
	}
	if regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`).MatchString(css) {
		t.Error("demos index CSS must use binding color tokens instead of raw hex colors")
	}
	if regexp.MustCompile(`font-size:\s*[0-9]`).MatchString(css) {
		t.Error("demos index CSS must use binding type tokens instead of raw font sizes")
	}
}

func TestWaterDemoUsesOnlyTheSharedSiteHeaderOffset(t *testing.T) {
	css := readDemoSource(t, "examples/gosx-docs/app/demos/water/page.css")
	if !strings.Contains(css, "--water-demo-chrome-offset: calc(var(--site-nav-offset, 61px) + 44px)") {
		t.Fatal("water stage must use the shared site navigation offset")
	}
	if strings.Contains(css, "--water-demo-chrome-offset: calc(var(--site-nav-offset) + 3.5rem)") {
		t.Fatal("water stage must not reserve space for the removed demo header")
	}
}

func TestScene3DShowcaseCSSHasNoOrphanedTail(t *testing.T) {
	css := readDemoSource(t, "examples/gosx-docs/app/demos/scene3d/page.css")
	if !strings.HasSuffix(strings.TrimSpace(css), "}") {
		t.Fatal("Scene3D showcase CSS must end in a complete rule")
	}
	for _, orphan := range []string{
		"\n    max-width: calc(100% - 2 * var(--space-md));",
		"\n    padding: var(--space-sm);",
	} {
		if strings.HasSuffix(strings.TrimSpace(css), strings.TrimSpace(orphan)) {
			t.Errorf("Scene3D showcase CSS retains orphaned declaration %q", orphan)
		}
	}
}

func readDemoSource(t *testing.T, relative string) string {
	t.Helper()
	value, err := os.ReadFile(repoPath(t, relative))
	if err != nil {
		t.Fatal(err)
	}
	return string(value)
}
