//go:build !js || !wasm

package bundle

import (
	"strings"

	"m31labs.dev/gosx/engine"
)

// PostEffectDiagnostics reports requested effects the native renderer cannot
// execute. An unsupported selected signal is skipped, never replaced with color.
func PostEffectDiagnostics(b engine.RenderBundle) []engine.RenderDiagnostic {
	return appendPostEffectDiagnostics(nil, b)
}

func appendPostEffectDiagnostics(out []engine.RenderDiagnostic, b engine.RenderBundle) []engine.RenderDiagnostic {
	for _, effect := range b.PostEffects {
		if strings.EqualFold(strings.TrimSpace(effect.Kind), "bloom") && strings.EqualFold(strings.TrimSpace(effect.Source), "specular") {
			return append(out, engine.RenderDiagnostic{
				Severity: "warning", Code: "scene.native.specular_bloom_unsupported", Backend: "native",
				Message: "specular bloom requires a separate radiance attachment; skipped on the native renderer",
			})
		}
	}
	return out
}
