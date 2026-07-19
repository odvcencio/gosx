package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQualityLadderEmptyEmitsNothing(t *testing.T) {
	p := Props{} // no QualityLadder
	ir := p.SceneIR()
	if len(ir.QualityLadder) != 0 {
		t.Errorf("empty QualityLadder should produce empty ir.QualityLadder, got %d", len(ir.QualityLadder))
	}
	if ir.QualityStartRung != 0 {
		t.Errorf("empty QualityLadder should leave QualityStartRung at 0, got %d", ir.QualityStartRung)
	}
	payload, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "qualityLadder") {
		t.Errorf("empty QualityLadder must not appear in the wire JSON: %s", payload)
	}
	bundle := ir.legacyProps()
	if bundle != nil {
		if _, exists := bundle["qualityLadder"]; exists {
			t.Errorf("empty QualityLadder must not emit qualityLadder key in legacy bundle")
		}
	}
}

func TestQualityLadderRoundTrip(t *testing.T) {
	p := Props{
		QualityLadder: []QualityRung{
			{Name: "raw", ExpensivePassCadence: 1},
			{
				Name:                 "glow",
				PostEffects:          []string{"bloom", "toneMapping"},
				LayerGroups:          []string{"particles", "far-decor"},
				ComputeBudgetScale:   0.5,
				ExpensivePassCadence: 2,
			},
		},
		QualityStartRung: 1,
	}
	ir := p.SceneIR()
	if len(ir.QualityLadder) != 2 {
		t.Fatalf("ir.QualityLadder len = %d, want 2", len(ir.QualityLadder))
	}
	if ir.QualityStartRung != 1 {
		t.Errorf("ir.QualityStartRung = %d, want 1", ir.QualityStartRung)
	}

	raw := ir.QualityLadder[0]
	if raw.Name != "raw" {
		t.Errorf("rung[0].Name = %q, want %q", raw.Name, "raw")
	}
	if len(raw.PostEffects) != 0 {
		t.Errorf("rung[0].PostEffects = %v, want empty (raw floor)", raw.PostEffects)
	}
	// ComputeBudgetScale zero-value idiom: unset means full budget (1.0),
	// matching Bloom.Scale/SSAO.Bias elsewhere in this package.
	if raw.ComputeBudgetScale != 1 {
		t.Errorf("rung[0].ComputeBudgetScale = %v, want 1 (unset -> full budget default)", raw.ComputeBudgetScale)
	}

	glow := ir.QualityLadder[1]
	if glow.Name != "glow" {
		t.Errorf("rung[1].Name = %q, want %q", glow.Name, "glow")
	}
	if len(glow.PostEffects) != 2 || glow.PostEffects[0] != "bloom" || glow.PostEffects[1] != "toneMapping" {
		t.Errorf("rung[1].PostEffects = %v, want [bloom toneMapping]", glow.PostEffects)
	}
	if len(glow.LayerGroups) != 2 || glow.LayerGroups[0] != "particles" || glow.LayerGroups[1] != "far-decor" {
		t.Errorf("rung[1].LayerGroups = %v, want [particles far-decor]", glow.LayerGroups)
	}
	if glow.ComputeBudgetScale != 0.5 {
		t.Errorf("rung[1].ComputeBudgetScale = %v, want 0.5", glow.ComputeBudgetScale)
	}
	if glow.ExpensivePassCadence != 2 {
		t.Errorf("rung[1].ExpensivePassCadence = %v, want 2", glow.ExpensivePassCadence)
	}

	// JSON wire shape.
	payload, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"qualityLadder":[`) {
		t.Fatalf("expected qualityLadder in wire JSON: %s", payload)
	}
	if !strings.Contains(string(payload), `"qualityStartRung":1`) {
		t.Fatalf("expected qualityStartRung:1 in wire JSON: %s", payload)
	}

	// legacyProps map-tree path must stay in sync with the JSON path.
	bundle := ir.legacyProps()
	rawList, ok := bundle["qualityLadder"].([]map[string]any)
	if !ok {
		t.Fatalf("bundle.qualityLadder type = %T, want []map[string]any", bundle["qualityLadder"])
	}
	if len(rawList) != 2 {
		t.Fatalf("legacy qualityLadder len = %d, want 2", len(rawList))
	}
	if rawList[1]["name"] != "glow" {
		t.Errorf(`legacy qualityLadder[1].name = %v, want "glow"`, rawList[1]["name"])
	}
	if bundle["qualityStartRung"] != 1 {
		t.Errorf("legacy qualityStartRung = %v, want 1", bundle["qualityStartRung"])
	}
}

func TestQualityRungBlankNameGetsIndexFallback(t *testing.T) {
	p := Props{QualityLadder: []QualityRung{{}, {Name: "  "}}}
	ir := p.SceneIR()
	if ir.QualityLadder[0].Name != "rung-0" {
		t.Errorf("rung[0].Name = %q, want %q", ir.QualityLadder[0].Name, "rung-0")
	}
	if ir.QualityLadder[1].Name != "rung-1" {
		t.Errorf("rung[1].Name = %q, want %q", ir.QualityLadder[1].Name, "rung-1")
	}
}

func TestQualityRungClampsOutOfRangeValues(t *testing.T) {
	p := Props{QualityLadder: []QualityRung{
		{Name: "over", ComputeBudgetScale: 4, ExpensivePassCadence: -3},
		{Name: "under", ComputeBudgetScale: -2, ExpensivePassCadence: 0},
	}}
	ir := p.SceneIR()
	if ir.QualityLadder[0].ComputeBudgetScale != 1 {
		t.Errorf("over-range ComputeBudgetScale = %v, want clamped to 1", ir.QualityLadder[0].ComputeBudgetScale)
	}
	if ir.QualityLadder[0].ExpensivePassCadence != 1 {
		t.Errorf("negative ExpensivePassCadence = %v, want clamped to 1", ir.QualityLadder[0].ExpensivePassCadence)
	}
	if ir.QualityLadder[1].ComputeBudgetScale != 0 {
		t.Errorf("under-range ComputeBudgetScale = %v, want clamped to 0", ir.QualityLadder[1].ComputeBudgetScale)
	}
	if ir.QualityLadder[1].ExpensivePassCadence != 1 {
		t.Errorf("zero ExpensivePassCadence = %v, want clamped to 1 (every frame)", ir.QualityLadder[1].ExpensivePassCadence)
	}
}

func TestQualityStartRungClampsOutOfRangeAtLowering(t *testing.T) {
	p := Props{
		QualityLadder:    []QualityRung{{Name: "a"}, {Name: "b"}},
		QualityStartRung: 99,
	}
	ir := p.SceneIR()
	if ir.QualityStartRung != 1 {
		t.Errorf("out-of-range QualityStartRung lowered to %d, want clamped to 1 (last index)", ir.QualityStartRung)
	}

	negative := Props{
		QualityLadder:    []QualityRung{{Name: "a"}, {Name: "b"}},
		QualityStartRung: -5,
	}
	ir2 := negative.SceneIR()
	if ir2.QualityStartRung != 0 {
		t.Errorf("negative QualityStartRung lowered to %d, want clamped to 0", ir2.QualityStartRung)
	}
}

func TestQualityLadderWarningsCombinedWithAdaptiveQuality(t *testing.T) {
	on := true
	p := Props{
		QualityLadder:   []QualityRung{{Name: "a"}},
		AdaptiveQuality: &on,
	}
	warnings := p.QualityLadderWarnings()
	if len(warnings) == 0 {
		t.Fatal("expected a warning when QualityLadder and AdaptiveQuality=true are both authored")
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "AdaptiveQuality") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want one mentioning AdaptiveQuality", warnings)
	}
}

func TestQualityLadderWarningsCleanWhenAdaptiveQualityAbsent(t *testing.T) {
	p := Props{QualityLadder: []QualityRung{{Name: "a", ComputeBudgetScale: 0.5, ExpensivePassCadence: 1}}}
	if warnings := p.QualityLadderWarnings(); len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}

func TestQualityLadderWarningsFlagOutOfRangeValues(t *testing.T) {
	p := Props{
		QualityLadder: []QualityRung{
			{Name: "a", ComputeBudgetScale: 2, ExpensivePassCadence: 0},
		},
		QualityStartRung: 5,
	}
	warnings := p.QualityLadderWarnings()
	if len(warnings) < 3 {
		t.Fatalf("expected warnings for ComputeBudgetScale, ExpensivePassCadence, and QualityStartRung, got %v", warnings)
	}
}

func TestQualityLadderWarningsEmptyForNoLadder(t *testing.T) {
	on := true
	p := Props{AdaptiveQuality: &on} // no ladder at all
	if warnings := p.QualityLadderWarnings(); len(warnings) != 0 {
		t.Errorf("no QualityLadder authored should never warn, got %v", warnings)
	}
}

// --- Layer tagging (Mesh.QualityGroup / ObjectIR.QualityGroup) ---

func TestMeshQualityGroupLowersToObjectIR(t *testing.T) {
	g := Graph{Nodes: []Node{
		Mesh{ID: "far-star", Geometry: SphereGeometry{Radius: 1}, QualityGroup: "particles"},
		Mesh{ID: "hero", Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1}}, // untagged
	}}
	ir := g.SceneIR()
	if len(ir.Objects) != 2 {
		t.Fatalf("ir.Objects len = %d, want 2", len(ir.Objects))
	}
	if ir.Objects[0].QualityGroup != "particles" {
		t.Errorf("objects[0].QualityGroup = %q, want %q", ir.Objects[0].QualityGroup, "particles")
	}
	if ir.Objects[1].QualityGroup != "" {
		t.Errorf("untagged mesh QualityGroup = %q, want empty", ir.Objects[1].QualityGroup)
	}

	payload, err := json.Marshal(ir.Objects[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"qualityGroup":"particles"`) {
		t.Fatalf("expected qualityGroup in wire JSON: %s", payload)
	}
	payload2, err := json.Marshal(ir.Objects[1])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload2), "qualityGroup") {
		t.Fatalf("untagged mesh must not emit qualityGroup key: %s", payload2)
	}

	legacy := ir.Objects[0].legacyProps()
	if legacy["qualityGroup"] != "particles" {
		t.Errorf("legacy objects[0].qualityGroup = %v, want %q", legacy["qualityGroup"], "particles")
	}
}
