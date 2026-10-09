//go:build js && wasm

package wasm

import (
	"context"
	"fmt"
	"m31labs.dev/gosx/client/jsutil"
	"syscall/js"
)

// IsCurrent reports whether GoSX still owns this mounted engine instance.
func (c Context) IsCurrent() bool {
	if !c.value.Truthy() {
		return false
	}
	if c.value.Get("isCurrent").Type() != js.TypeFunction {
		return true
	}
	return c.value.Call("isCurrent").Bool()
}

// Navigate uses GoSX enhanced navigation and its document fallback. Cancellation
// stops waiting; a navigation already committed can dispose the calling engine.
func (c Context) Navigate(ctx context.Context, target string, options NavigationOptions) (err error) {
	if ctx == nil {
		return fmt.Errorf("navigation context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("navigate: %v", failure)
		}
	}()
	if c.value.Get("navigate").Type() != js.TypeFunction {
		return fmt.Errorf("navigation bridge is unavailable")
	}
	opts, err := encodeEventDetail(options)
	if err != nil {
		return err
	}
	_, err = jsutil.AwaitPromiseContext(ctx, c.value.Call("navigate", target, opts))
	return err
}
