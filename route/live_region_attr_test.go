package route

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// These tests render a .gsx fixture through the real pipeline — gosx.Compile
// (which runs ir.Validate) followed by RenderProgramComponent — the same
// entry points `gosx check` and `gosx render` use. See
// compileCountdownFixture in route/countdown_attr_test.go for the precedent
// this follows (gosx#178, gosx#217).

// TestFileRendererLiveAttrsSurviveRender covers data-gosx-live-src,
// data-gosx-live-interval, data-gosx-live-bind, and
// data-gosx-live-flash-class (gosx#217): all four must reach the served
// HTML untouched, alongside the server-rendered initial text the runtime
// keeps fresh after the first poll.
func TestFileRendererLiveAttrsSurviveRender(t *testing.T) {
	html := compileCountdownFixture(t, "\t\t<div data-gosx-live-src=\"/api/live/week\" "+
		"data-gosx-live-interval=\"10s\">\n"+
		"\t\t\t<span data-gosx-live-bind=\"score:t42\" data-gosx-live-flash-class=\"score-flash\">0.0</span>\n"+
		"\t\t</div>")

	for _, want := range []string{
		`data-gosx-live-src="/api/live/week"`,
		`data-gosx-live-interval="10s"`,
		`data-gosx-live-bind="score:t42"`,
		`data-gosx-live-flash-class="score-flash"`,
		">0.0<",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in rendered live-region html %q", want, html)
		}
	}
}

// TestFileRendererRegionAttrsSurviveRender is the same round-trip proof for
// data-gosx-region-interval (gosx#217), composing with the pre-existing
// data-gosx-region / data-gosx-region-url pair.
func TestFileRendererRegionAttrsSurviveRender(t *testing.T) {
	html := compileCountdownFixture(t, "\t\t<ul data-gosx-region data-gosx-region-url=\"/api/wire/events\" "+
		"data-gosx-region-interval=\"20s\">\n"+
		"\t\t\t<li>Server-rendered signal</li>\n"+
		"\t\t</ul>")

	for _, want := range []string{
		`data-gosx-region`,
		`data-gosx-region-url="/api/wire/events"`,
		`data-gosx-region-interval="20s"`,
		">Server-rendered signal<",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in rendered region-fragment html %q", want, html)
		}
	}
}

// TestCompileRejectsInvalidStaticLiveInterval is the compile-path proof for
// data-gosx-live-interval (gosx#217), mirroring
// TestCompileRejectsInvalidStaticCountdownInstant in countdown_attr_test.go.
func TestCompileRejectsInvalidStaticLiveInterval(t *testing.T) {
	src := "package docs\n\n" +
		"func Page() Node {\n" +
		"\treturn <main>\n" +
		"\t\t<div data-gosx-live-src=\"/api/live/week\" data-gosx-live-interval=\"1h\"></div>\n" +
		"\t</main>\n" +
		"}\n"
	_, err := gosx.Compile([]byte(src))
	if err == nil {
		t.Fatal("expected compile to reject an invalid data-gosx-live-interval value")
	}
	if !strings.Contains(err.Error(), "data-gosx-live-interval") {
		t.Fatalf("expected the live-interval diagnostic in the compile error, got %v", err)
	}
}

// TestCompileRejectsInvalidStaticRegionInterval is the same end-to-end proof
// for data-gosx-region-interval.
func TestCompileRejectsInvalidStaticRegionInterval(t *testing.T) {
	src := "package docs\n\n" +
		"func Page() Node {\n" +
		"\treturn <main>\n" +
		"\t\t<div data-gosx-region data-gosx-region-url=\"/api/wire/events\" data-gosx-region-interval=\"not-a-duration\"></div>\n" +
		"\t</main>\n" +
		"}\n"
	_, err := gosx.Compile([]byte(src))
	if err == nil {
		t.Fatal("expected compile to reject an invalid data-gosx-region-interval value")
	}
	if !strings.Contains(err.Error(), "data-gosx-region-interval") {
		t.Fatalf("expected the region-interval diagnostic in the compile error, got %v", err)
	}
}

// TestCompileRejectsInvalidStaticLiveBind is the same end-to-end proof for
// data-gosx-live-bind.
func TestCompileRejectsInvalidStaticLiveBind(t *testing.T) {
	src := "package docs\n\n" +
		"func Page() Node {\n" +
		"\treturn <main>\n" +
		"\t\t<span data-gosx-live-bind=\"score t42\"></span>\n" +
		"\t</main>\n" +
		"}\n"
	_, err := gosx.Compile([]byte(src))
	if err == nil {
		t.Fatal("expected compile to reject an invalid data-gosx-live-bind value")
	}
	if !strings.Contains(err.Error(), "data-gosx-live-bind") {
		t.Fatalf("expected the live-bind diagnostic in the compile error, got %v", err)
	}
}
