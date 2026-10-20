//go:build js && wasm

package scene

import "m31labs.dev/selena"

func compileSelenaCached(source []byte, opts selena.CompileOptions) (selena.Result, error) {
	return selena.Compile(source, opts)
}
