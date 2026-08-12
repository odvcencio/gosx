//go:build js && wasm && !gosx_tiny_islands_only

package main

import runtimewasm "m31labs.dev/gosx/client/runtime/wasm"

func runtimeVariant() runtimewasm.Variant {
	return runtimewasm.VariantFull
}
