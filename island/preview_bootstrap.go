package island

import "sync/atomic"

// previewBootstrapEnabled is the process-level flag controlling whether
// pages opt into the cross-frame relay's "preview-mode" bootstrap. See
// ADR 0009 (decisions/0009-iframe-transport-postmessage-relay.md) and plan
// section C of plans/2026-05-26-iframe-cross-frame-signal-transport.md.
//
// The flag is process-level (not per-Renderer) because the storefront
// layout opts in via a single init() call (`island.EnablePreviewBootstrap()`),
// and that storefront layout is shared across all routes including the ones
// loaded inside the editor preview iframe. Per-Renderer plumbing would
// require threading the flag through every route's binding setup. A single
// atomic is enough — preview mode is binary, not per-page configurable.
var previewBootstrapEnabled int32

// EnablePreviewBootstrap opts every Renderer constructed thereafter into
// emitting a minimal cross-frame relay bootstrap (islands runtime + the
// relay.js JS module), even when the page registers no islands. The
// runtime activates only when the storefront detects it's loaded inside
// a preview iframe (query param `gosx-preview=1` or `window.parent !==
// window` with a matching editor origin).
//
// Idempotent — safe to call multiple times. Storefront layouts should call
// this once in init() if they want to be previewable.
func EnablePreviewBootstrap() {
	atomic.StoreInt32(&previewBootstrapEnabled, 1)
}

// ResetPreviewBootstrap clears the flag. Test-only entrypoint.
func ResetPreviewBootstrap() {
	atomic.StoreInt32(&previewBootstrapEnabled, 0)
}

// PreviewBootstrapEnabled reports the current state of the flag. Exported
// for the cms/route surfaces that need to check whether to wire in the
// preview-mode query parameter handling.
func PreviewBootstrapEnabled() bool {
	return atomic.LoadInt32(&previewBootstrapEnabled) == 1
}
