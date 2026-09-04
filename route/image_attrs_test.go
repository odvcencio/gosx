package route

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// These tests render a .gsx <Image> fixture through the real pipeline —
// gosx.Compile followed by RenderProgramComponent — to prove priority and
// responsive are consumed into server.ImageProps rather than falling through
// to imageExtraAttrs and leaking as literal <img priority responsive>
// attributes (gosx#199).

func compileImageFixture(t *testing.T, attrs string) string {
	t.Helper()
	src := "package docs\n\n" +
		"func Page() Node {\n" +
		"\treturn <Image " + attrs + " />\n" +
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

// TestFileRendererImagePriorityFlipsLoadingAndFetchPriority covers gosx#199:
// <Image priority /> must set loading="eager" and fetchpriority="high" on
// the emitted <img>, and priority itself must never appear as a literal
// attribute (it is not a valid HTML attribute name).
func TestFileRendererImagePriorityFlipsLoadingAndFetchPriority(t *testing.T) {
	html := compileImageFixture(t, `src="/hero.png" alt="Hero" priority`)

	for _, snippet := range []string{
		`loading="eager"`,
		`fetchpriority="high"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
	if strings.Contains(html, " priority") {
		t.Fatalf("expected priority to be consumed, not leaked as a literal attribute, got %q", html)
	}
	if strings.Contains(html, `loading="lazy"`) {
		t.Fatalf("expected priority to override the default lazy loading, got %q", html)
	}
}

// TestFileRendererImageWithoutPriorityStaysLazyAndOmitsFetchPriority proves
// the default (no priority attribute) path is unaffected: lazy loading, and
// no fetchpriority attribute at all (server.Image only emits fetchpriority
// when FetchPriority is set explicitly or Priority is true).
func TestFileRendererImageWithoutPriorityStaysLazyAndOmitsFetchPriority(t *testing.T) {
	html := compileImageFixture(t, `src="/hero.png" alt="Hero"`)

	if !strings.Contains(html, `loading="lazy"`) {
		t.Fatalf("expected default lazy loading, got %q", html)
	}
	if strings.Contains(html, "fetchpriority") {
		t.Fatalf("expected no fetchpriority attribute without priority, got %q", html)
	}
}

// TestFileRendererImageResponsiveWiresAutoWidthsAndSizes covers gosx#199:
// <Image responsive /> must reach server.ImageProps.Responsive (auto width
// ladder from AutoImageWidths, and sizes="100vw" when sizes is unset), and
// responsive itself must never appear as a literal attribute.
func TestFileRendererImageResponsiveWiresAutoWidthsAndSizes(t *testing.T) {
	html := compileImageFixture(t, `src="/hero.jpg" alt="Hero" width={800} responsive`)

	for _, snippet := range []string{
		`srcset="/_gosx/image?`,
		`w=320`,
		`w=800`,
		`sizes="100vw"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
	if strings.Contains(html, " responsive") {
		t.Fatalf("expected responsive to be consumed, not leaked as a literal attribute, got %q", html)
	}
}

// TestFileRendererImageWithoutResponsiveOmitsSrcset proves the default (no
// responsive attribute, no explicit widths) path stays a plain <img>: no
// srcset is generated just because width is set.
func TestFileRendererImageWithoutResponsiveOmitsSrcset(t *testing.T) {
	html := compileImageFixture(t, `src="/hero.jpg" alt="Hero" width={800}`)

	if strings.Contains(html, "srcset=") {
		t.Fatalf("expected no srcset without responsive or widths, got %q", html)
	}
}

// TestFileRendererImageExplicitSizesWinsOverResponsiveDefault proves an
// author-provided sizes attribute is not clobbered by responsive's 100vw
// default.
func TestFileRendererImageExplicitSizesWinsOverResponsiveDefault(t *testing.T) {
	html := compileImageFixture(t, `src="/hero.jpg" alt="Hero" width={800} responsive sizes="50vw"`)

	if !strings.Contains(html, `sizes="50vw"`) {
		t.Fatalf("expected explicit sizes to win, got %q", html)
	}
	if strings.Contains(html, `sizes="100vw"`) {
		t.Fatalf("expected responsive default sizes not to override explicit sizes, got %q", html)
	}
}

func TestFileRendererImageSourcesEnableConsumerBackedArtDirection(t *testing.T) {
	src := `package docs

func Page() Node {
	return <Image src={data.hero} alt="Ceramic bowl" width={1600} height={1200} sources={data.sources} priority />
}
`
	prog, err := gosx.Compile([]byte(src))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Values: map[string]any{
		"data": map[string]any{
			"hero": "https://images.example.com/hero.jpg",
			"sources": []map[string]any{
				{"media": "(max-width: 600px)", "srcset": "https://images.example.com/hero-mobile.jpg 800w", "sizes": "100vw", "type": "image/jpeg"},
			},
		},
	}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, snippet := range []string{
		`<picture>`,
		`<source srcset="https://images.example.com/hero-mobile.jpg 800w" media="(max-width: 600px)" sizes="100vw" type="image/jpeg" />`,
		`<img src="https://images.example.com/hero.jpg" alt="Ceramic bowl" width="1600" height="1200" loading="eager" decoding="async" fetchpriority="high" />`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
	if strings.Contains(html, " sources=") {
		t.Fatalf("expected sources to be consumed, not leaked as an HTML attribute, got %q", html)
	}
}
