package playground

import (
	"testing"

	gosx "github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/ir"
)

// expectedSlugs is the canonical list of preset slugs the suite enforces.
var expectedSlugs = []string{
	"counter",
	"two-counters",
	"toggle",
	"greeter",
	"shared-theme",
}

func TestPresetsNonEmpty(t *testing.T) {
	got := Presets()
	if len(got) < 5 {
		t.Fatalf("Presets() returned %d entries, want >= 5", len(got))
	}
}

func TestPresetsAllHaveSlugTitleSource(t *testing.T) {
	for i, p := range Presets() {
		if p.Slug == "" {
			t.Errorf("preset[%d] has empty Slug", i)
		}
		if p.Title == "" {
			t.Errorf("preset[%d] (%s) has empty Title", i, p.Slug)
		}
		if p.Source == "" {
			t.Errorf("preset[%d] (%s) has empty Source", i, p.Slug)
		}
	}
}

func TestPresetsHaveUniqueSlugs(t *testing.T) {
	seen := make(map[string]int)
	for i, p := range Presets() {
		if prev, ok := seen[p.Slug]; ok {
			t.Errorf("duplicate slug %q at index %d and %d", p.Slug, prev, i)
		}
		seen[p.Slug] = i
	}
}

func TestPresetsAllCompileAndLower(t *testing.T) {
	for _, p := range Presets() {
		p := p // capture
		t.Run(p.Slug, func(t *testing.T) {
			prog, err := gosx.Compile([]byte(p.Source))
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}
			if len(prog.Components) < 1 {
				t.Fatalf("Compile returned 0 components")
			}
			if !prog.Components[0].IsIsland {
				t.Fatalf("Components[0].IsIsland == false")
			}

			island, err := ir.LowerIsland(prog, 0)
			if err != nil {
				t.Fatalf("LowerIsland error: %v", err)
			}
			if len(island.Nodes) == 0 {
				t.Fatalf("LowerIsland returned 0 nodes")
			}
		})
	}
}

func TestPresetBySlugHappy(t *testing.T) {
	p, ok := PresetBySlug("counter")
	if !ok {
		t.Fatal("PresetBySlug(\"counter\") returned ok=false")
	}
	if p.Slug != "counter" {
		t.Fatalf("expected slug counter, got %q", p.Slug)
	}
	if p.Title == "" {
		t.Fatal("counter preset has empty Title")
	}
}

func TestPresetBySlugMiss(t *testing.T) {
	p, ok := PresetBySlug("nonexistent")
	if ok {
		t.Fatalf("expected ok=false for nonexistent slug, got preset %+v", p)
	}
	if p != (Preset{}) {
		t.Fatalf("expected zero Preset for miss, got %+v", p)
	}
}

func TestDefaultPresetMatchesFirst(t *testing.T) {
	all := Presets()
	if len(all) == 0 {
		t.Fatal("Presets() returned empty slice")
	}
	def := DefaultPreset()
	if def != all[0] {
		t.Fatalf("DefaultPreset() != Presets()[0]:\n  got  %+v\n  want %+v", def, all[0])
	}
}

// TestPresetsDefensiveCopy verifies that mutating the returned slice does not
// affect subsequent calls to Presets().
func TestPresetsDefensiveCopy(t *testing.T) {
	first := Presets()
	if len(first) == 0 {
		t.Fatal("empty presets")
	}
	slug := first[0].Slug
	first[0] = Preset{Slug: "mutated"}

	second := Presets()
	if second[0].Slug != slug {
		t.Fatalf("Presets() is not returning a defensive copy: got %q want %q", second[0].Slug, slug)
	}
}
