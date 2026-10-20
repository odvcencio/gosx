package strictcheck

import (
	"path/filepath"
	"testing"
)

// TestRequiredReachabilityWarnsOnVisuallyHiddenRequiredControl
// reconstructs the gosx#249 premise-table signup-form defect: a required
// radio input carries a class the project's own stylesheet visually hides
// with the classic "position: absolute; width: 1px; height: 1px" sr-only
// idiom. A browser refuses to submit a form containing a required control
// it cannot focus, so this makes the whole form unsubmittable, silently.
func TestRequiredReachabilityWarnsOnVisuallyHiddenRequiredControl(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "public", "styles.css"), `
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
}
`)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<input class="sr-only" type="radio" name="badge" required />`))
	warnings := checkFileWarnings(t, filepath.Join(dir, "page.gsx"))
	if !hasWarningContaining(warnings, "heuristic") || !hasWarningContaining(warnings, ".sr-only") {
		t.Fatalf("expected a heuristic warning naming .sr-only, got: %+v", warnings)
	}
}

func TestRequiredReachabilityWarnsOnDisplayNone(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "public", "styles.css"), `.hidden-field { display: none; }`)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<input class="hidden-field" type="text" name="honeypot" required />`))
	warnings := checkFileWarnings(t, filepath.Join(dir, "page.gsx"))
	if !hasWarningContaining(warnings, "display: none") {
		t.Fatalf("expected a warning citing display: none, got: %+v", warnings)
	}
}

// TestRequiredReachabilityAcceptsVisibleRequiredControl proves an ordinary
// required control with no matching hiding rule produces no warning.
func TestRequiredReachabilityAcceptsVisibleRequiredControl(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "public", "styles.css"), `
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
}
.field { border: 1px solid #ccc; padding: 4px; }
`)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<input class="field" type="text" name="email" required />`))
	warnings := checkFileWarnings(t, filepath.Join(dir, "page.gsx"))
	if len(warnings) != 0 {
		t.Fatalf("expected no warning for a visible required control, got: %+v", warnings)
	}
}

// TestRequiredReachabilityAcceptsRequiredControlWithNoPublicCSS proves the
// check degrades to a no-op, not a false positive, when the project has no
// public/*.css at all -- the same posture publicImageDirFor documents for
// check 1's local-source rule.
func TestRequiredReachabilityAcceptsRequiredControlWithNoPublicCSS(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<input class="sr-only" type="text" name="email" required />`))
	warnings := checkFileWarnings(t, filepath.Join(dir, "page.gsx"))
	if len(warnings) != 0 {
		t.Fatalf("expected no warning with no public/ CSS present, got: %+v", warnings)
	}
}

// TestRequiredReachabilityAcceptsRequiredControlWithDynamicClass proves a
// dynamic {expr} class is out of scope rather than guessed at.
func TestRequiredReachabilityAcceptsRequiredControlWithDynamicClass(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "public", "styles.css"), `.sr-only { display: none; }`)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<input class={dynamicClassName()} type="text" name="email" required />`))
	warnings := checkFileWarnings(t, filepath.Join(dir, "page.gsx"))
	if len(warnings) != 0 {
		t.Fatalf("expected no warning for a dynamic class, got: %+v", warnings)
	}
}
