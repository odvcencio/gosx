//go:build !js || !wasm

package bundle

import (
	"strings"

	"m31labs.dev/gosx/engine"
)

// PostEffectDiagnostics reports requested effects the native renderer cannot
// execute. An unsupported selected signal is skipped, never replaced with color.
//
// It also reports every custom post pass the native renderer does not run, one
// diagnostic per pass in declaration order: a pass with no WGSL source is
// skipped, and a pass with WGSL source is an identity passthrough because the
// only native GPU device is the headless CPU rasterizer, which runs no custom
// shaders. The two drop sites in resolveNativePostFXEffects and
// recordNativePostFXPasses rely on this function to make that drop visible.
func PostEffectDiagnostics(b engine.RenderBundle) []engine.RenderDiagnostic {
	return appendPostEffectDiagnostics(nil, b)
}

// A new diagnostic code added here must also be added to the filter in
// withPostEffectDiagnostics (scene/preview/preview.go), or the preview reports
// it twice. TestWithPostEffectDiagnosticsReportsEachCodeOnce checks the filter.
func appendPostEffectDiagnostics(out []engine.RenderDiagnostic, b engine.RenderBundle) []engine.RenderDiagnostic {
	for _, effect := range b.PostEffects {
		switch strings.ToLower(strings.TrimSpace(effect.Kind)) {
		case "bloom":
			if strings.EqualFold(strings.TrimSpace(effect.Source), "specular") {
				out = append(out, engine.RenderDiagnostic{
					Severity: "warning", Code: "scene.native.specular_bloom_unsupported", Backend: "native",
					Message: "specular bloom requires a separate radiance attachment; skipped on the native renderer",
				})
			}
		case "custompost":
			name := effect.Name
			if name == "" {
				name = "custom"
			}
			if strings.TrimSpace(effect.FragmentWGSL) == "" {
				out = append(out, engine.RenderDiagnostic{
					Severity: "warning", Code: "scene.native.custom_post_skipped", Backend: "native", Target: name,
					Message: "custom post pass \"" + name + "\" has no WGSL source; skipped on the native renderer",
				})
				continue
			}
			out = append(out, engine.RenderDiagnostic{
				Severity: "warning", Code: "scene.native.custom_post_passthrough", Backend: "native", Target: name,
				Message: "custom post pass \"" + name + "\" is not executed: the native renderer has no GPU device outside a browser, " +
					"so on the headless device the pass is an identity passthrough and its shader does not run",
			})
		}
	}
	return out
}
