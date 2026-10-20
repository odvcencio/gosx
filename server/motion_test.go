package server

import (
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/motion"
)

func TestMotionRendersManagedBootstrapContract(t *testing.T) {
	respectReduced := false
	html := gosx.RenderHTML(Motion(MotionProps{
		Tag:                  "section",
		Preset:               MotionPresetSlideUp,
		Trigger:              MotionTriggerView,
		Duration:             360,
		Delay:                40,
		Easing:               "ease-out",
		Distance:             24,
		RespectReducedMotion: &respectReduced,
	}, gosx.Attrs(gosx.Attr("class", "hero-copy")), gosx.Text("Animated copy")))

	for _, snippet := range []string{
		`<section`,
		`class="hero-copy"`,
		`data-gosx-motion`,
		`data-gosx-enhance="motion"`,
		`data-gosx-enhance-layer="bootstrap"`,
		`data-gosx-fallback="html"`,
		`data-gosx-motion-preset="slide-up"`,
		`data-gosx-motion-trigger="view"`,
		`data-gosx-motion-duration="360"`,
		`data-gosx-motion-delay="40"`,
		`data-gosx-motion-easing="ease-out"`,
		`data-gosx-motion-distance="24"`,
		`data-gosx-motion-respect-reduced="false"`,
		`data-gosx-motion-state="idle"`,
		`Animated copy`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestMotionScopeSerializesProgramAndKeepsHTMLFallback(t *testing.T) {
	program := motion.NewProgram("server-rendered")
	progress := program.ScrollProgress("progress", "#stage", motion.AxisY)
	program.BindCSSVariable(progress, "#card", "--progress", "")
	program.PinTo("#scene", "pin", "#pin")
	html := gosx.RenderHTML(MotionScope(program, gosx.Attrs(gosx.Attr("class", "motion-scope")), gosx.Text("Visible before motion")))
	for _, snippet := range []string{
		`class="motion-scope"`,
		`data-gosx-enhance="motion"`,
		`data-gosx-enhance-layer="bootstrap"`,
		`data-gosx-fallback="html"`,
		`data-gosx-motion-program=`,
		`&#34;kind&#34;:&#34;scroll&#34;`,
		`Visible before motion`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestMotionReducedPolicyDefaultsToSkipAndCanFade(t *testing.T) {
	defaultHTML := gosx.RenderHTML(Motion(MotionProps{}, gosx.Text("Copy")))
	if !strings.Contains(defaultHTML, `data-gosx-motion-reduced-policy="skip"`) {
		t.Fatalf("default reduced-motion policy missing from %q", defaultHTML)
	}
	fadeHTML := gosx.RenderHTML(Motion(MotionProps{ReducedMotionPolicy: motion.ReducedMotionFade}, gosx.Text("Copy")))
	if !strings.Contains(fadeHTML, `data-gosx-motion-reduced-policy="fade"`) {
		t.Fatalf("fade reduced-motion policy missing from %q", fadeHTML)
	}
}

func TestMotionProgramHelperReportsInvalidProgramWithoutHidingContent(t *testing.T) {
	html := gosx.RenderHTML(MotionScope(motion.NewProgram(""), gosx.Text("Still visible")))
	if !strings.Contains(html, `data-gosx-motion-error="invalid-program"`) || !strings.Contains(html, "Still visible") {
		t.Fatalf("invalid program fallback = %q", html)
	}
}

func TestMotionProgramTweenDurationUsesSeconds(t *testing.T) {
	program := motion.NewProgram("duration")
	program.Tween("fade", 0, 1, 250*time.Millisecond, motion.Ease{}, motion.ReducedMotionSkip)
	data, err := program.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"duration":0.25`) {
		t.Fatalf("duration wire value = %s", data)
	}
}

func TestMotionDefaultsToFadeLoadWithReducedMotionRespected(t *testing.T) {
	html := gosx.RenderHTML(Motion(MotionProps{}, gosx.Text("Animated copy")))

	for _, snippet := range []string{
		`data-gosx-motion-preset="fade"`,
		`data-gosx-motion-trigger="load"`,
		`data-gosx-motion-duration="220"`,
		`data-gosx-motion-delay="0"`,
		`data-gosx-motion-easing="cubic-bezier(0.16, 1, 0.3, 1)"`,
		`data-gosx-motion-distance="18"`,
		`data-gosx-motion-respect-reduced="true"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}
