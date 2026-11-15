//go:build js && wasm && gosx_selena_compiler

package scene

import "m31labs.dev/selena"

// compileSelenaCached compiles without a cache in the browser. It exists only
// when a js/wasm build opts into the compiler with -tags gosx_selena_compiler.
func compileSelenaCached(source []byte, opts selena.CompileOptions, modules []string) (selena.Result, error) {
	return selenaCompile(source, opts, modules)
}
