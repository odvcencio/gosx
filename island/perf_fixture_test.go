package island

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestPerfFixtureRendererKeepsCompatibilityDebtAndGlobalState(t *testing.T) {
	for _, mode := range []string{"configured", "preview", "lite-missing", "selective-missing", "full-unconfigured"} {
		t.Run(mode, func(t *testing.T) {
			_, manifest := perfAssetRendererFixture(t)
			before, _ := json.Marshal(manifest)
			r, err := NewPerfFixtureRenderer(manifest, mode)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "lite-missing" {
				r.EnableBootstrap()
			} else if mode != "preview" {
				r.RenderIsland("Counter", nil, gosx.Text("Count: 0"))
			}
			uses, err := r.PerfAssetUses(PerfAssetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			monolith := perfAssetByID(t, uses, "framework/runtime/bootstrap.js")
			if (monolith.Phase == "startup") != (mode != "configured" && mode != "preview") {
				t.Fatal("compatibility monolith was dropped or selected unnecessarily")
			}
			if mode == "full-unconfigured" && perfAssetByID(t, uses, "framework/runtime/full.wasm").Phase != "startup" {
				t.Fatal("explicit full fallback was dropped")
			}
			if mode == "preview" && (r.Summary().BootstrapMode != "preview" || perfAssetByID(t, uses, "framework/runtime/relay.js").Phase != "startup") {
				t.Fatal("preview relay contract missing")
			}
			if PreviewBootstrapEnabled() {
				t.Fatal("fixture changed global preview")
			}
			ordinary, err := NewPerfFixtureRenderer(manifest, "configured")
			if err != nil || ordinary.Summary().BootstrapMode != "none" {
				t.Fatal("fixture mode leaked to another renderer", err)
			}
			after, _ := json.Marshal(manifest)
			if string(before) != string(after) || len(uses.Assets) != len(manifest.PerfAssetUses.Assets) {
				t.Fatal("fixture altered or omitted the build inventory")
			}
		})
	}
}

func TestPerfFixtureRendererRejectsUnsupportedModeAndLegacyGraph(t *testing.T) {
	_, manifest := perfAssetRendererFixture(t)
	if _, err := NewPerfFixtureRenderer(manifest, "private-mode"); err == nil || strings.Contains(err.Error(), "private-mode") {
		t.Fatal("unsupported mode accepted or echoed", err)
	}
	manifest.PerfAssetUses = nil
	if _, err := NewPerfFixtureRenderer(manifest, "configured"); err == nil {
		t.Fatal("unknown inventory accepted as production fixture")
	}
}
