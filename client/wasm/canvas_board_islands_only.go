//go:build js && wasm && gosx_tiny_islands_only

// Canvas2D paint-loop JS bridge — islands-only stub.
//
// The tiny build omits the CanvasBoardAdapter and the render/bundle dependency
// entirely (the canvas2d surface kind is unavailable — its hydrate path returns
// a clean error via bridge_canvasboard_islands.go). This stub keeps the
// registration entry point present so main.registerRuntime compiles
// identically across build flavors, but installs no globals: the islands-only
// runtime intentionally exposes none of the __gosx_*_canvas functions (asserted
// by TestIslandsOnlyRuntimeOmitsFullRuntimeExports).

package main

import "m31labs.dev/gosx/client/bridge"

func registerCanvasBoardRuntime(b *bridge.Bridge) {}
