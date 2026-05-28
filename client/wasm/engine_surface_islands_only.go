//go:build js && wasm && gosx_tiny_islands_only

// Engine-surface JS bridge — islands-only stub.
//
// The tiny build omits the engine/surface dependency entirely (the
// CanvasHostReceiver, the JS canvas wrapper, the IR lowering, …). The
// bridge-side HydrateEngineSurface in this build returns a clean
// "unavailable" error; this file ensures the JS-side entry points
// still exist (so the bootstrap doesn't see a missing-function
// signature) but are no-ops.

package main

import "m31labs.dev/gosx/client/bridge"

func registerEngineSurfaceRuntime(b *bridge.Bridge) {}
