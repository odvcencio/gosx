//go:build !(js && wasm)

package scene

// Publish sends the pool to the browser renderer. It does nothing outside a
// js/wasm build and reports false.
func (p *DecalPool) Publish() bool { return false }
