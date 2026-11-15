//go:build js && wasm

package bundle

import "m31labs.dev/gosx/engine"

// Native material diagnostics do not belong to the browser renderer contract.
// Scene3D browser backends publish their own per-draw render truth.
func MaterialDiagnostics(engine.RenderBundle) []engine.RenderDiagnostic { return nil }

type materialDiagnosticsCache struct{}

func (*materialDiagnosticsCache) update(engine.RenderBundle) []engine.RenderDiagnostic { return nil }

// PostEffectDiagnostics reports nothing in the browser build, for the same
// reason as MaterialDiagnostics. It exists so package scene/preview compiles.
// A preview result built in a js/wasm build therefore carries no post-effect
// diagnostics.
func PostEffectDiagnostics(engine.RenderBundle) []engine.RenderDiagnostic { return nil }
