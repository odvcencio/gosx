package server

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/motion"
)

func TestMotionScopeCompilesFixedCSSByDefaultAndCanDisable(t *testing.T) {
	program := motion.NewProgram("scope-css")
	scroll := program.ScrollProgress("scroll", "", motion.AxisY)
	mapped := program.Map("mapped", scroll, 0, 1, 0, 1)
	program.BindCSSVariable(mapped, "#card", "--progress", "")

	html := gosx.RenderHTML(MotionScope(program, gosx.Text("Always visible")))
	for _, snippet := range []string{
		`data-gosx-motion-scope="scope-css"`,
		`<style>`,
		`animation-timeline: scroll(root block)`,
		`Always visible`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in compiled scope %q", snippet, html)
		}
	}
	if !strings.Contains(html, "data-gosx-motion-program") || !strings.Contains(html, "cssCompiled") {
		t.Fatalf("the full program must still ship with its cssCompiled list for browsers without scroll timelines: %q", html)
	}
	// Style text is not decoded as HTML, so the selector quotes must stay raw.
	if !strings.Contains(html, "[data-gosx-motion-scope='scope-css']") {
		t.Fatalf("style text was HTML-escaped or missing its scoped selector: %q", html)
	}

	disabled := false
	noCSS := gosx.RenderHTML(MotionScopeWithOptions(program, MotionScopeOptions{CompileCSS: &disabled}, gosx.Text("Fallback")))
	if strings.Contains(noCSS, "@supports (animation-timeline") || !strings.Contains(noCSS, "data-gosx-motion-program") {
		t.Fatalf("disabled CSS compilation should preserve runtime program: %q", noCSS)
	}
}

func TestMotionScopeNeverLetsProgramIDsCloseTheStyleElement(t *testing.T) {
	program := motion.NewProgram("bad</style><script>alert(1)</script>")
	scroll := program.ScrollProgress("scroll", "", motion.AxisY)
	mapped := program.Map("mapped", scroll, 0, 1, 0, 1)
	program.BindCSSVariable(mapped, "#card", "--progress", "")
	html := gosx.RenderHTML(MotionScope(program, gosx.Text("x")))
	if strings.Contains(html, "<style>") || strings.Contains(html, "<script>") {
		t.Fatalf("a program ID with markup must not produce a style element or script: %q", html)
	}
}
