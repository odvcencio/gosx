package docs

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestDemosIndexLinksToAccessibleScene3DShowreel(t *testing.T) {
	page := readDemoSource(t, "examples/gosx-docs/app/demos/page.gsx")
	if strings.Contains(page, "<Scene3D ") {
		t.Fatal("demos index must defer the Scene3D mount to its own route")
	}
	if strings.Contains(page, "<main") {
		t.Error("demos index must not nest a main landmark inside the application main")
	}
	for _, required := range []string{
		`<section class="demos-landing" aria-labelledby="demos-landing-title">`,
		`id="demos-landing-title"`,
		`aria-labelledby="demos-showreel-title"`,
		`aria-describedby="demos-showreel-description"`,
		`aria-label="Illustration of an orbital sculpture"`,
		`href="/demos/showreel"`,
		`data.showcase`,
		`data.additional`,
		`demoSourceURL(demo.SourcePath)`,
	} {
		if !strings.Contains(page, required) {
			t.Errorf("demos index missing rendered showreel contract %q", required)
		}
	}
	if strings.Contains(page, "<script") {
		t.Error("demos index must not add bespoke script behavior")
	}
	if got := strings.Count(page, `data-gosx-link="true"`); got != 6 {
		t.Errorf("demos index managed-navigation link declarations = %d, want 6", got)
	}
	if strings.Contains(page, `target="_blank" data-gosx-link="true"`) {
		t.Error("external source links must not be intercepted by managed navigation")
	}
	showreel := readDemoSource(t, "examples/gosx-docs/app/demos/showreel/page.gsx")
	if !strings.Contains(showreel, "<Scene3D {...data.scene} />") || !strings.Contains(showreel, `aria-label="Interactive orbital sculpture.`) {
		t.Error("dedicated showreel route must mount an accessible Scene3D")
	}
}

func TestDemosIndexStylesHonorTokensResponsiveLayoutAndReducedMotion(t *testing.T) {
	css := readDemoSource(t, "examples/gosx-docs/app/demos/page.css")
	for _, required := range []string{
		`var(--font-display)`,
		`var(--color-accent)`,
		`var(--space-xl)`,
		`@media (max-width: 700px)`,
		`@media (prefers-reduced-motion: reduce)`,
		`.demos-showreel__canvas`,
		`[data-gosx-scene3d-renderer]::after`,
		`attr(data-gosx-scene3d-renderer)`,
		`attr(data-gosx-scene3d-renderer-fallback)`,
		`overflow-x: hidden`,
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
