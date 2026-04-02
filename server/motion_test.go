package server

import (
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
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
