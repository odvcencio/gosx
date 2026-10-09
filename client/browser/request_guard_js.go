//go:build js && wasm

package browser

import (
	"fmt"
	"syscall/js"
)

// RequestGuard owns a host-side request policy. It retains no Go callback and
// adds no WASM crossings to fetch, enhanced request, or sendBeacon calls.
type RequestGuard struct {
	value    js.Value
	disposed bool
}

func GuardRequests(policy RequestPolicy) (guard *RequestGuard, err error) {
	defer func() {
		if r := recover(); r != nil {
			guard = nil
			err = fmt.Errorf("browser: request guard: %v", r)
		}
	}()
	global := js.Global()
	if win := global.Get("window"); win.Truthy() {
		global = win
	}
	namespace := global.Get("__gosx")
	if !namespace.Truthy() {
		return nil, fmt.Errorf("browser: request guard runtime unavailable")
	}
	host := namespace.Get("host")
	if !host.Truthy() {
		return nil, fmt.Errorf("browser: request guard runtime unavailable")
	}
	requests := host.Get("requests")
	if !requests.Truthy() {
		return nil, fmt.Errorf("browser: request guard runtime unavailable")
	}
	installer := requests.Get("guard")
	if installer.Type() != js.TypeFunction {
		return nil, fmt.Errorf("browser: request guard runtime unavailable")
	}
	paths := make([]any, len(policy.BlockedPaths))
	for i, path := range policy.BlockedPaths {
		paths[i] = path
	}
	return &RequestGuard{value: installer.Invoke(map[string]any{"blockedPaths": paths})}, nil
}

func (g *RequestGuard) Dispose() {
	if g == nil || g.disposed {
		return
	}
	g.disposed = true
	defer func() { _ = recover() }()
	g.value.Call("dispose")
	g.value = js.Undefined()
}
