package preview

import (
	"testing"

	"m31labs.dev/gosx/engine"
)

// TestWithPostEffectDiagnosticsReportsEachCodeOnce keeps the code filter in
// withPostEffectDiagnostics in step with render/bundle. The bundle below makes
// PostEffectDiagnostics emit every code it has. The frame already carries those
// same diagnostics, as BundleIR leaves them. A code that the filter does not
// strip would appear twice. A new code added to PostEffectDiagnostics needs an
// effect here and, if the test fails, an entry in the filter.
func TestWithPostEffectDiagnosticsReportsEachCodeOnce(t *testing.T) {
	frame := engine.RenderBundle{PostEffects: []engine.RenderPostEffect{
		{Kind: "customPost", Name: "withShader", FragmentWGSL: "fn f() {}"},
		{Kind: "customPost", Name: "noShader"},
		{Kind: "bloom", Source: "specular"},
	}}
	fromRenderer := withPostEffectDiagnostics(frame)
	if len(fromRenderer) != 3 {
		t.Fatalf("diagnostics = %+v, want one per effect", fromRenderer)
	}
	// Simulate BundleIR having already added them.
	frame.Diagnostics = append([]engine.RenderDiagnostic{{Code: "scene.preview.other"}}, fromRenderer...)
	got := withPostEffectDiagnostics(frame)
	counts := map[string]int{}
	for _, d := range got {
		counts[d.Code]++
	}
	for _, code := range []string{
		"scene.native.custom_post_passthrough",
		"scene.native.custom_post_skipped",
		"scene.native.specular_bloom_unsupported",
		"scene.preview.other",
	} {
		if counts[code] != 1 {
			t.Fatalf("code %s appears %d times in %+v, want once", code, counts[code], got)
		}
	}
}
