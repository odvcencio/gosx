//go:build js && wasm && (gosx_tiny_islands_only || gosx_runtime_core || gosx_runtime_collab)

package main

import "m31labs.dev/gosx/client/bridge"

func registerEngineRuntime(b *bridge.Bridge) {}
