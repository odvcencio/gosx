//go:build js && wasm

package jsutil

import (
	"context"
	"fmt"
	"syscall/js"
)

// WaitForAttribute waits for a mounted DOM attribute without polling. The
// observer and Go callback are released on completion or cancellation. Call
// from a goroutine, and cancel ctx when the owning engine is disposed.
func WaitForAttribute(ctx context.Context, element js.Value, name, value string) (err error) {
	if ctx == nil {
		return fmt.Errorf("jsutil: attribute context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !element.Truthy() || name == "" {
		return fmt.Errorf("jsutil: element and attribute name are required")
	}
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("wait for attribute: %v", failure)
		}
	}()
	matches := func() bool {
		actual := element.Call("getAttribute", name)
		return actual.Type() == js.TypeString && actual.String() == value
	}
	if matches() {
		return nil
	}
	ready := make(chan struct{}, 1)
	callback := js.FuncOf(func(js.Value, []js.Value) any {
		if matches() {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
		return nil
	})
	defer callback.Release()
	observer := js.Global().Get("MutationObserver").New(callback)
	defer observer.Call("disconnect")
	observer.Call("observe", element, map[string]any{"attributes": true, "attributeFilter": []any{name}})
	// Also covers an attribute changed synchronously while observation began.
	if matches() {
		return nil
	}
	select {
	case <-ready:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
