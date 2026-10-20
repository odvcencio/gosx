//go:build !js || !wasm

package bundle

import (
	"testing"

	"m31labs.dev/gosx/engine"
)

func TestNativeSelectiveBloomDoesNotBloomDiffuse(t *testing.T) {
	for _, source := range []string{"", "color", "specular", " SPECULAR "} {
		b := engine.RenderBundle{PostEffects: []engine.RenderPostEffect{{Kind: "bloom", Source: source, Intensity: .3}}}
		selected := source == "specular" || source == " SPECULAR "
		if resolveBloomConfig(b).enabled == selected {
			t.Fatalf("source %q: native must skip unsupported selection, preserving ordinary bloom", source)
		}
		warnings := PostEffectDiagnostics(b)
		if selected && (len(warnings) != 1 || warnings[0].Code != "scene.native.specular_bloom_unsupported") {
			t.Fatal("unsupported selection must be observable")
		}
		if !selected && len(warnings) != 0 {
			t.Fatal("ordinary color bloom is supported")
		}
		var cache materialDiagnosticsCache
		if len(cache.update(b)) != len(warnings) {
			t.Fatal("frame diagnostics lost post effect fallback")
		}
		if len(cache.update(engine.RenderBundle{})) != 0 {
			t.Fatal("removed effect left stale diagnostics")
		}
	}
}
