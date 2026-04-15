package css

import (
	"strings"
	"testing"
)

func TestExtractScene3DStylesStripsBrowserCSSAndParsesRules(t *testing.T) {
	source := `
.scene { color: white; }
@scene3d {
  Scene3D.hero {
    scene-background: #101820;
    environment-ambient-intensity: 0.33 !important;
  }
  @media (min-width: 800px) {
    Mesh.primary, Points.stars {
      material-color: #8de1ff;
      spin-y: 0.04;
    }
  }
}
.caption { color: gray; }
`
	cssText, sheet := ExtractScene3DStyles(source)
	if strings.Contains(cssText, "@scene3d") || strings.Contains(cssText, "scene-background") {
		t.Fatalf("expected @scene3d block stripped from browser CSS, got %q", cssText)
	}
	if !strings.Contains(cssText, ".scene") || !strings.Contains(cssText, ".caption") {
		t.Fatalf("expected normal CSS preserved, got %q", cssText)
	}
	if got := len(sheet.Rules); got != 2 {
		t.Fatalf("expected 2 Scene3D rules, got %d: %#v", got, sheet.Rules)
	}
	if sheet.Rules[0].Selector != "Scene3D.hero" {
		t.Fatalf("unexpected first selector %#v", sheet.Rules[0].Selector)
	}
	if sheet.Rules[0].Declarations[1].Value != "0.33" {
		t.Fatalf("expected !important stripped from declaration, got %#v", sheet.Rules[0].Declarations[1])
	}
	if sheet.Rules[1].Selector != "Mesh.primary, Points.stars" {
		t.Fatalf("unexpected nested selector %#v", sheet.Rules[1].Selector)
	}
}

func TestExtractScene3DStylesMirrorsSceneFilterForBrowserCSS(t *testing.T) {
	source := `
.galaxy-scene {
  scene-filter: bloom(threshold 0.8 intensity 1.1) vignette(intensity 0.5);
}
.not-a-property-scene-filter { color: white; }
`
	cssText, sheet := ExtractScene3DStyles(source)
	if len(sheet.Rules) != 0 {
		t.Fatalf("expected no @scene3d rules, got %#v", sheet.Rules)
	}
	if strings.Contains(cssText, "\n  scene-filter:") {
		t.Fatalf("expected scene-filter declaration mirrored away, got %q", cssText)
	}
	if !strings.Contains(cssText, "--scene-filter: bloom(threshold 0.8 intensity 1.1)") {
		t.Fatalf("expected --scene-filter custom property in %q", cssText)
	}
	if !strings.Contains(cssText, ".not-a-property-scene-filter") {
		t.Fatalf("expected selector text preserved in %q", cssText)
	}
}
