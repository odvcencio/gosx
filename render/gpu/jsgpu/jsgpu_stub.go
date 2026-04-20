//go:build !js || !wasm

package jsgpu

import "github.com/odvcencio/gosx/render/gpu"

// Open on non-WASM builds returns gpu.ErrUnsupported. The jsgpu backend only
// compiles under js && wasm; this stub exists so server-side or test code can
// import the package without a build-tag dance.
func Open(canvasID string) (gpu.Device, gpu.Surface, error) {
	_ = canvasID
	return nil, nil, gpu.ErrUnsupported
}
