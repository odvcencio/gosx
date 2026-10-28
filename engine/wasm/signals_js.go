//go:build js && wasm

package wasm

import (
	"encoding/json"
	"fmt"
	"sync"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

// SetSignal writes a JSON value into the page's shared GoSX signal runtime.
// Unlike signal.NewShared's native value, this crosses the engine/VM boundary.
//
// The page notifies its subscribers before the write returns. Go subscribers
// (SubscribeSignal) run after the page has notified the others, and before
// SetSignal returns; they do not run inside the call into JavaScript.
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
	result := jsutil.Call(c.value, "setSignal", name, encoded)
	if result.Type() == js.TypeString && result.String() != "" {
		return fmt.Errorf("set shared signal: %s", result.String())
	}
	return nil
}

// SubscribeSignal receives JSON from subsequent writes to a page shared signal.
// Dispose the returned HandleFunc to detach and release the Go callback.
//
// When Go itself writes the signal (SetSignal, SetSignals), the handler runs
// after the page has notified its other subscribers and before the write
// returns. A write from page code reaches the handler inside the page's own
// notification, as before.
func (c Context) SubscribeSignal(name string, handler func(json.RawMessage)) (dispose HandleFunc, err error) {
	return c.subscribeSignal(name, handler, false)
}

// subscribeSignalReplay is SubscribeSignal that also delivers the signal's
// current value once. ServeCalls uses it so a call written before the engine
// subscribed is answered. On the main thread the page delivers the current
// value inside the subscribing call; the handler runs for it after that call
// has returned, before this function returns, and a signal with no value
// delivers JSON null. In a worker it arrives later, as a message, and a signal
// with no value delivers nothing.
func (c Context) subscribeSignalReplay(name string, handler func(json.RawMessage)) (HandleFunc, error) {
	return c.subscribeSignal(name, handler, true)
}

func (c Context) subscribeSignal(name string, handler func(json.RawMessage), replay bool) (dispose HandleFunc, err error) {
	if handler == nil {
		return nil, fmt.Errorf("shared signal handler is required")
	}
	var callback *jsutil.Callback
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("subscribe shared signal: %v", recovered)
		}
		if err != nil && callback != nil {
			callback.Release()
		}
	}()
	method := "subscribeSignalJSON"
	subscribe := c.value.Get(method)
	rawJSON := subscribe.Type() == js.TypeFunction
	if !rawJSON {
		method = "subscribeSignal"
		subscribe = c.value.Get(method)
	}
	if subscribe.Type() != js.TypeFunction {
		return nil, fmt.Errorf("shared signal bridge is unavailable")
	}
	// The callback is deferred while Go is inside a call into the page (see
	// jsutil.Callback). Hosts without subscribeSignalJSON hand over a decoded
	// value, which Go encodes when the deferred call runs.
	callback = jsutil.NewCallback(func(_ js.Value, args []js.Value) {
		if len(args) == 0 {
			return
		}
		encoded := args[0]
		if !rawJSON {
			encoded = js.Global().Get("JSON").Call("stringify", encoded)
		}
		if encoded.Type() == js.TypeString {
			handler(json.RawMessage(encoded.String()))
		}
	})
	unsubscribe := jsutil.Call(c.value, method, name, callback.Value(), map[string]any{"immediate": replay})
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
	return c.setSignals(values, false)
}

// SetChangedSignals publishes a state snapshot as one batch, notifying only
// signals whose current JSON value differs. Unlike SetSignals, an identical
// write is not an event. Use SetSignals for repeated event/pulse deliveries.
// Comparison happens in the shared store, so intervening island writes and
// in-place edits are observed rather than hidden by a sender-side cache.
func (c Context) SetChangedSignals(values map[string]any) error {
	return c.setSignals(values, true)
}

func (c Context) setSignals(values map[string]any, changedOnly bool) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("set shared signals: %v", failure)
		}
	}()
	if values == nil {
		values = map[string]any{}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	if c.value.Get("setSignalsJSON").Type() == js.TypeFunction {
		result := jsutil.Call(c.value, "setSignalsJSON", string(encoded), changedOnly)
		if result.Type() == js.TypeString && result.String() != "" {
			return fmt.Errorf("set shared signals: %s", result.String())
		}
		return nil
	}
	// Older hosts accept decoded values and always notify. Keep this compatible
	// fallback rather than caching writes that another island may have changed.
	if c.value.Get("setSignals").Type() != js.TypeFunction {
		return fmt.Errorf("shared signal batch bridge is unavailable")
	}
	value := js.Global().Get("JSON").Call("parse", string(encoded))
	result := jsutil.Call(c.value, "setSignals", value)
	if result.Type() == js.TypeString && result.String() != "" {
		return fmt.Errorf("set shared signals: %s", result.String())
	}
	return nil
}
