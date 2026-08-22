package docs

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

func readOrreryPageSource(t *testing.T) string {
	t.Helper()
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	return string(source)
}

func readOrreryCSS(t *testing.T) string {
	t.Helper()
	css, err := os.ReadFile("page.css")
	if err != nil {
		t.Fatal(err)
	}
	return string(css)
}

// TestOrreryTelemetryDoesNotDependOnBespokeScriptOrCSSHas keeps the renderer
// status honest through the shared Scene3D bindings only.
func TestOrreryTelemetryDoesNotDependOnBespokeScriptOrCSSHas(t *testing.T) {
	css := readOrreryCSS(t)
	if strings.Contains(css, ":has(") {
		t.Fatal("telemetry must remain truthful in browsers without CSS :has()")
	}
	for _, marker := range []string{
		".orrery__canvas .gosx-scene3d-unsupported",
		"place-items: center",
		"text-align: center",
	} {
		if !strings.Contains(css, marker) {
			t.Errorf("intentional renderer fallback styling missing %q", marker)
		}
	}

	page := readOrreryPageSource(t)
	for _, marker := range []string{
		`data-gosx-scene3d-status-scope`,
		`data-gosx-scene3d-status="renderer"`,
		`data-gosx-scene3d-status="fallback"`,
		`data-gosx-scene3d-status="quality"`,
	} {
		if !strings.Contains(page, marker) {
			t.Errorf("telemetry binding missing %q", marker)
		}
	}
	if strings.Contains(page, `<script`) {
		t.Fatal("the meridian must use shared Scene3D status bindings instead of bespoke JavaScript")
	}
}

// TestOrreryPublishesAnHonestInteractionContract pins the visible control
// guidance against what the program actually configures.
func TestOrreryPublishesAnHonestInteractionContract(t *testing.T) {
	page := readOrreryPageSource(t)
	for _, marker := range []string{
		"Drag or swipe to orbit",
		"scroll or pinch to zoom",
		"arrows explore",
		"+/− zoom",
		"Home restores the opening view",
		"This scene animates continuously by design.",
		"suppresses this canvas's animation loop",
	} {
		if !strings.Contains(page, marker) {
			t.Errorf("public interaction or motion guidance missing %q", marker)
		}
	}

	props := LodestarMeridianProgram()
	if props.Controls != scene.ControlOrbit || props.AutoRotate == nil || *props.AutoRotate {
		t.Fatalf("interaction must remain user-directed orbit: controls=%q autoRotate=%v", props.Controls, props.AutoRotate)
	}
	if props.ControlMinDistance >= props.ControlMaxDistance {
		t.Fatalf("orbit bounds inverted: %.1f..%.1f", props.ControlMinDistance, props.ControlMaxDistance)
	}
	message := strings.ToLower(props.UnsupportedMessage)
	if !strings.Contains(message, "unavailable") || !strings.Contains(message, "remain available") {
		t.Errorf("fallback copy must state what still works: %q", props.UnsupportedMessage)
	}
}

// TestOrreryOverlayNeverObstructsCanvasInput proves by construction that the
// overlay is a grid sibling of the canvas (not an absolutely positioned card
// floating above it), so pointer and keyboard canvas input stay unobstructed
// at every viewport.
func TestOrreryOverlayNeverObstructsCanvasInput(t *testing.T) {
	page := readOrreryPageSource(t)
	canvasAt := strings.Index(page, `class="orrery__canvas"`)
	mountAt := strings.Index(page, "<Scene3D")
	canvasCloseAt := strings.Index(page[canvasAt:], "</div>") + canvasAt
	overlayAt := strings.Index(page, `class="orrery__overlay"`)
	if !(canvasAt < mountAt && mountAt < canvasCloseAt && canvasCloseAt < overlayAt) {
		t.Fatalf("overlay is not a sibling after the closed canvas container (%d < %d < %d <= %d)",
			canvasAt, mountAt, canvasCloseAt, overlayAt)
	}

	css := readOrreryCSS(t)
	if !strings.Contains(css, "grid-template-columns: minmax(0, 1fr) minmax(18rem, 23rem);") {
		t.Error("desktop layout must place the overlay beside the canvas as a grid column")
	}
	narrow := strings.Index(css, "@media (max-width: 900px)")
	if narrow < 0 {
		t.Fatal("narrow layout must be defined")
	}
	tail := css[narrow:]
	if !strings.Contains(tail, "grid-template-columns: minmax(0, 1fr);") {
		t.Error("narrow layouts must stack the overlay into document flow below the canvas")
	}
	if strings.Contains(css, "position: absolute") {
		t.Error("the meridian overlay must never float over the canvas")
	}
}

// TestOrreryChoreographyCopyMatchesTheDeclaredCycle keeps the editorial phase
// legend aligned with the program's declared key instants.
func TestOrreryChoreographyCopyMatchesTheDeclaredCycle(t *testing.T) {
	page := readOrreryPageSource(t)
	for _, marker := range []string{
		"Ignition",
		"procession",
		"Transit",
		"13.2 s",
		"6 s, 8 s, and 12 s periods",
		"24 s",
	} {
		if !strings.Contains(strings.ToLower(page), strings.ToLower(marker)) &&
			!strings.Contains(page, marker) {
			t.Errorf("phase legend missing %q", marker)
		}
	}
	if !strings.Contains(page, "17 stable nodes") || !strings.Contains(page, "4 animation channels in 1 clip") {
		t.Error("budget list must match the declared scene budgets")
	}
}
