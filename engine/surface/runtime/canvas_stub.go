//go:build !(js && wasm)

package runtime

import "sync"

// registrationsMu guards the registered surface map on non-WASM builds.
// On WASM, canvas_js.go owns this state.
var (
	registrationsMu sync.Mutex
	registrations   = make(map[string]Surface)
)

// registerSurface stores s under name in the in-process registry.
// On native builds this is a no-op store; there is no browser to mount into.
func registerSurface(name string, s Surface) {
	registrationsMu.Lock()
	defer registrationsMu.Unlock()
	registrations[name] = s
}
