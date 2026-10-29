//go:build js && wasm

package scene

import "m31labs.dev/selena/bindings"

// js/wasm builds do not link the Selena compiler. Each function below has the
// signature of its native counterpart in selena_compile.go and returns
// ErrSelenaCompileUnavailable, so code that calls it still builds for the
// browser.

// CompileSelenaMaterial returns ErrSelenaCompileUnavailable in js/wasm builds.
func CompileSelenaMaterial(source []byte, opts SelenaMaterialOptions) (CustomMaterial, bindings.Layout, error) {
	return CustomMaterial{}, bindings.Layout{}, ErrSelenaCompileUnavailable
}

// CompileSelenaBundle returns ErrSelenaCompileUnavailable in js/wasm builds.
func CompileSelenaBundle(source []byte, materials ...SelenaMaterialOptions) ([]SelenaCompiledMaterial, error) {
	return nil, ErrSelenaCompileUnavailable
}

// CompileSelenaPoints returns ErrSelenaCompileUnavailable in js/wasm builds.
func CompileSelenaPoints(source []byte, opts SelenaMaterialOptions) (CustomMaterial, bindings.Layout, error) {
	return CustomMaterial{}, bindings.Layout{}, ErrSelenaCompileUnavailable
}

// CompileSelenaParticleRender returns ErrSelenaCompileUnavailable in js/wasm
// builds.
func CompileSelenaParticleRender(source []byte, opts SelenaMaterialOptions) (CustomMaterial, bindings.Layout, error) {
	return CustomMaterial{}, bindings.Layout{}, ErrSelenaCompileUnavailable
}

// CompileSelenaPost returns ErrSelenaCompileUnavailable in js/wasm builds.
func CompileSelenaPost(source []byte, opts SelenaMaterialOptions) (CustomMaterial, bindings.Layout, error) {
	return CustomMaterial{}, bindings.Layout{}, ErrSelenaCompileUnavailable
}
