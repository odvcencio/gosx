//go:build js && wasm && !gosx_tiny_islands_only && !gosx_runtime_core && !gosx_runtime_engine && !gosx_runtime_collab

package main

import runtimewasm "m31labs.dev/gosx/client/runtime/wasm"

func runtimeVariant() runtimewasm.Variant {
	return runtimewasm.VariantFull
}
