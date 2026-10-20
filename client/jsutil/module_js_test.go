//go:build js && wasm

package jsutil

import (
	"syscall/js"
	"testing"
)

func TestImportModuleUsesFrameworkRuntime(t *testing.T) {
	before := js.Global().Get("__gosx")
	defer js.Global().Set("__gosx", before)
	var requested string
	load := js.FuncOf(func(_ js.Value, args []js.Value) any {
		requested = args[0].String()
		return js.Global().Get("Promise").Call("resolve", map[string]any{"exported": true})
	})
	defer load.Release()
	js.Global().Set("__gosx", map[string]any{"runtime": map[string]any{"modules": map[string]any{"import": load}}})
	module, err := ImportModule("/.proxy/assets/sdk.js")
	if err != nil || !module.Get("exported").Bool() || requested != "/.proxy/assets/sdk.js" {
		t.Fatalf("module import failed: %v %q", err, requested)
	}
	js.Global().Delete("__gosx")
	if _, err := ImportModule("/sdk.js"); err == nil {
		t.Fatal("missing framework runtime was accepted")
	}
	if _, err := ImportModule(" "); err == nil {
		t.Fatal("empty module URL was accepted")
	}
}
