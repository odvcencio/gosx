//go:build js && wasm && gosx_runtime_collab

package main

import runtimewasm "m31labs.dev/gosx/client/runtime/wasm"

func runtimeVariant() runtimewasm.Variant {
	return runtimewasm.VariantCollab
}
