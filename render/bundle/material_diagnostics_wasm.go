//go:build js && wasm

package bundle

import "m31labs.dev/gosx/engine"

// Native material diagnostics do not belong to the browser renderer contract.
// Scene3D browser backends publish their own per-draw render truth.
func MaterialDiagnostics(engine.RenderBundle) []engine.RenderDiagnostic { return nil }
