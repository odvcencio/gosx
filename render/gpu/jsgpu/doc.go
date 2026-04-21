// Package jsgpu is the WebGPU backend for render/gpu, implemented against
// syscall/js and navigator.gpu.
//
// Use Open to acquire a Device bound to a canvas element; Device satisfies
// gpu.Device and all returned resources satisfy their corresponding gpu
// interfaces. Non-WASM builds include a stub file that makes Open return
// gpu.ErrUnsupported so the package is importable everywhere.
package jsgpu
