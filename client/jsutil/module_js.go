//go:build js && wasm

package jsutil

import (
	"errors"
	"strings"
	"syscall/js"
)

// ImportModule imports a public HTTP(S) ES module URL through the GoSX engine
// runtime. Relative URLs resolve against document.baseURI. Pass an already
// prefixed public URL when deploying beneath a base path, as for engine assets.
// Concurrent imports of one URL share their promise; a failed load may retry.
// Call from a goroutine, as with AwaitPromise. Module exports stay browser-owned.
func ImportModule(url string) (js.Value, error) {
	if strings.TrimSpace(url) == "" {
		return js.Undefined(), errors.New("jsutil: module URL is required")
	}
	api := js.Global().Get("__gosx")
	for _, key := range []string{"runtime", "modules"} {
		if !api.Truthy() {
			return js.Undefined(), errors.New("jsutil: GoSX module runtime is not loaded")
		}
		api = api.Get(key)
	}
	if !api.Truthy() || api.Get("import").Type() != js.TypeFunction {
		return js.Undefined(), errors.New("jsutil: GoSX module runtime is not loaded")
	}
	return AwaitPromise(api.Call("import", url))
}
