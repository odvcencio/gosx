//go:build js && wasm

package wasm

import (
	"encoding/json"
	"fmt"
	"sync"
	"syscall/js"
)

// SetSignal writes a JSON value into the page's shared GoSX signal runtime.
// Unlike signal.NewShared's native value, this crosses the engine/VM boundary.
func (c Context) SetSignal(name string, value any) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("set shared signal: %v", recovered)
		}
	}()
	setter := c.value.Get("setSignal")
	if setter.Type() != js.TypeFunction {
		return fmt.Errorf("shared signal bridge is unavailable")
	}
	encoded, err := encodeEventDetail(value)
	if err != nil {
		return err
	}
	result := c.value.Call("setSignal", name, encoded)
	if result.Type() == js.TypeString && result.String() != "" {
		return fmt.Errorf("set shared signal: %s", result.String())
	}
	return nil
}

// SubscribeSignal receives JSON from subsequent writes to a page shared signal.
// Dispose the returned HandleFunc to detach and release the Go callback.
func (c Context) SubscribeSignal(name string, handler func(json.RawMessage)) (dispose HandleFunc, err error) {
	if handler == nil {
		return nil, fmt.Errorf("shared signal handler is required")
	}
	var callback js.Func
	created := false
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("subscribe shared signal: %v", recovered)
		}
		if err != nil && created {
			callback.Release()
		}
	}()
	subscribe := c.value.Get("subscribeSignal")
	if subscribe.Type() != js.TypeFunction {
		return nil, fmt.Errorf("shared signal bridge is unavailable")
	}
	callback = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		encoded := js.Global().Get("JSON").Call("stringify", args[0])
		if encoded.Type() == js.TypeString {
			handler(json.RawMessage(encoded.String()))
		}
		return nil
	})
	created = true
	unsubscribe := c.value.Call("subscribeSignal", name, callback, map[string]any{"immediate": false})
	if unsubscribe.Type() != js.TypeFunction {
		return nil, fmt.Errorf("shared signal subscription is unavailable")
	}
	var once sync.Once
	return func() {
		once.Do(func() { defer callback.Release(); unsubscribe.Invoke() })
	}, nil
}

// SignalJSON returns a snapshot of the current shared signal.
func (c Context) SignalJSON(name string) (data json.RawMessage, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("read shared signal: %v", failure)
		}
	}()
	if c.value.Get("getSignal").Type() != js.TypeFunction {
		return nil, fmt.Errorf("shared signal reader is unavailable")
	}
	value := c.value.Call("getSignal", name)
	if value.IsUndefined() {
		return nil, ErrSignalNotFound
	}
	encoded := js.Global().Get("JSON").Call("stringify", value)
	if encoded.Type() != js.TypeString {
		return nil, fmt.Errorf("shared signal is not JSON serializable")
	}
	return json.RawMessage(encoded.String()), nil
}

// SetSignals publishes all values as one cross-island update. Serialization
// completes before any write, and subscribers observe the complete batch.
func (c Context) SetSignals(values map[string]any) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("set shared signals: %v", failure)
		}
	}()
	if values == nil {
		values = map[string]any{}
	}
	encoded, err := encodeEventDetail(values)
	if err != nil {
		return err
	}
	if c.value.Get("setSignals").Type() != js.TypeFunction {
		return fmt.Errorf("shared signal batch bridge is unavailable")
	}
	result := c.value.Call("setSignals", encoded)
	if result.Type() == js.TypeString && result.String() != "" {
		return fmt.Errorf("set shared signals: %s", result.String())
	}
	return nil
}
