//go:build js && wasm

package browser

import (
	"context"
	"fmt"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

// Fetch awaits headers and body bytes. Call from a goroutine when initiated
// by a browser callback. Cancellation aborts the network request and releases
// Go promise handlers even if the transport or body promise never settles.
func Fetch(ctx context.Context, request FetchRequest) (response FetchResponse, err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("browser: fetch: %v", value)
		}
	}()
	if err = fetchContext(ctx); err != nil {
		return response, err
	}
	if js.Global().Get("fetch").Type() != js.TypeFunction {
		return response, ErrFetchUnavailable
	}
	method := request.Method
	if method == "" {
		method = "GET"
	}
	options := map[string]any{"method": method}
	headers := map[string]any{}
	if request.ContentType != "" {
		headers["Content-Type"] = request.ContentType
	}
	for name, value := range request.Headers {
		headers[name] = value
	}
	if len(headers) > 0 {
		options["headers"] = headers
	}
	if request.Body != nil {
		options["body"] = jsutil.NewUint8ArrayFromBytes(request.Body)
	}
	done := make(chan struct{})
	defer close(done)
	var controller js.Value
	// Cancellation must abort before Fetch returns, even if the watcher has
	// not been scheduled yet. Calling AbortController.abort twice is safe.
	defer func() {
		if ctx.Err() != nil && controller.Type() == js.TypeObject {
			defer func() { _ = recover() }()
			controller.Call("abort")
		}
	}()
	if ctx.Done() != nil {
		ctor := js.Global().Get("AbortController")
		if ctor.Type() != js.TypeFunction {
			return response, fmt.Errorf("browser: fetch cancellation: %w", ErrFetchUnavailable)
		}
		controller = ctor.New()
		options["signal"] = controller.Get("signal")
		go func() {
			select {
			case <-ctx.Done():
				// A custom transport can throw on abort; the context still cancels the
				// promise wait and must not crash an unrelated goroutine.
				defer func() { _ = recover() }()
				controller.Call("abort")
			case <-done:
			}
		}()
	}
	value, err := jsutil.AwaitPromiseContext(ctx, js.Global().Call("fetch", request.URL, options))
	if err != nil {
		return response, fmt.Errorf("browser: fetch transport: %w", err)
	}
	response.Status = value.Get("status").Int()
	response.StatusText = value.Get("statusText").String()
	body, err := jsutil.AwaitPromiseContext(ctx, value.Call("arrayBuffer"))
	if err != nil {
		return response, fmt.Errorf("browser: reading response body: %w", err)
	}
	array := js.Global().Get("Uint8Array").New(body)
	response.Body = make([]byte, array.Get("byteLength").Int())
	js.CopyBytesToGo(response.Body, array)
	return response, nil
}
