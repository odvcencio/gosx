package route

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// These tests render a .gsx fixture through the real pipeline — gosx.Compile
// (which runs ir.Validate) followed by RenderProgramComponent — the same
// entry points `gosx check` and `gosx render` use. See
// route/managed_form_shorthand_test.go for the precedent this follows
// (gosx#179): a check that only exercises a Node-API-only helper can miss a
// regression in the actual .gsx render path.

func compileCountdownFixture(t *testing.T, body string) string {
	t.Helper()
	src := "package docs\n\n" +
		"func Page() Node {\n" +
		"\treturn <main>\n" +
		body + "\n" +
		"\t</main>\n" +
		"}\n"
	prog, err := gosx.Compile([]byte(src))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return html
}

// TestFileRendererCountdownCompactAttrsSurviveRender covers the compact
// render mode (gosx#178): data-gosx-countdown, data-gosx-countdown-format,
// data-gosx-countdown-warn and data-gosx-countdown-then must all reach the
// served HTML untouched, alongside the server-rendered initial text the
// runtime keeps moving after the first tick.
func TestFileRendererCountdownCompactAttrsSurviveRender(t *testing.T) {
	html := compileCountdownFixture(t, "\t\t<span data-gosx-countdown=\"2026-08-22T16:00:00-04:00\" "+
		"data-gosx-countdown-format=\"mm:ss\" data-gosx-countdown-warn=\"30s\" "+
		"data-gosx-countdown-then=\"revalidate\">15:00</span>")

	for _, want := range []string{
		`data-gosx-countdown="2026-08-22T16:00:00-04:00"`,
		`data-gosx-countdown-format="mm:ss"`,
		`data-gosx-countdown-warn="30s"`,
		`data-gosx-countdown-then="revalidate"`,
		">15:00<",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in rendered countdown html %q", want, html)
		}
	}
}

// TestFileRendererCountdownSegmentAttrsSurviveRender covers the segment
// render mode (gosx#178): the app owns the markup around each
// data-gosx-countdown-segment child, and the render path must not disturb
// any of it.
func TestFileRendererCountdownSegmentAttrsSurviveRender(t *testing.T) {
	html := compileCountdownFixture(t, "\t\t<div data-gosx-countdown=\"2026-08-22T16:00:00-04:00\">\n"+
		"\t\t\t<b data-gosx-countdown-segment=\"days\">03</b>\n"+
		"\t\t\t<b data-gosx-countdown-segment=\"hours\">04</b>\n"+
		"\t\t\t<b data-gosx-countdown-segment=\"minutes\">12</b>\n"+
		"\t\t\t<b data-gosx-countdown-segment=\"seconds\">09</b>\n"+
		"\t\t</div>")

	for _, want := range []string{
		`data-gosx-countdown="2026-08-22T16:00:00-04:00"`,
		`data-gosx-countdown-segment="days"`,
		`data-gosx-countdown-segment="hours"`,
		`data-gosx-countdown-segment="minutes"`,
		`data-gosx-countdown-segment="seconds"`,
		">03<", ">04<", ">12<", ">09<",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in rendered segment countdown html %q", want, html)
		}
	}
}

// TestCompileRejectsInvalidStaticCountdownInstant proves the check-time
// guard (ir/validate.go) is wired through the real gosx.Compile path that
// `gosx check` and `gosx render` both call, not only through a direct
// ir.Validate unit call.
func TestCompileRejectsInvalidStaticCountdownInstant(t *testing.T) {
	src := "package docs\n\n" +
		"func Page() Node {\n" +
		"\treturn <main>\n" +
		"\t\t<span data-gosx-countdown=\"not-a-real-instant\"></span>\n" +
		"\t</main>\n" +
		"}\n"
	_, err := gosx.Compile([]byte(src))
	if err == nil {
		t.Fatal("expected compile to reject an invalid data-gosx-countdown instant")
	}
	if !strings.Contains(err.Error(), "data-gosx-countdown") || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("expected the RFC3339 diagnostic in the compile error, got %v", err)
	}
}

// TestCompileRejectsInvalidStaticCountdownFormat is the same end-to-end
// proof for data-gosx-countdown-format.
func TestCompileRejectsInvalidStaticCountdownFormat(t *testing.T) {
	src := "package docs\n\n" +
		"func Page() Node {\n" +
		"\treturn <main>\n" +
		"\t\t<span data-gosx-countdown=\"2026-08-22T16:00:00-04:00\" data-gosx-countdown-format=\"hh:mm\"></span>\n" +
		"\t</main>\n" +
		"}\n"
	_, err := gosx.Compile([]byte(src))
	if err == nil {
		t.Fatal("expected compile to reject an invalid data-gosx-countdown-format value")
	}
	if !strings.Contains(err.Error(), "data-gosx-countdown-format") {
		t.Fatalf("expected the format diagnostic in the compile error, got %v", err)
	}
}
