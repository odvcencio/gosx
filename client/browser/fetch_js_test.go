//go:build js && wasm

package browser

import (
	"context"
	"errors"
	"m31labs.dev/gosx/client/jsutil"
	"strings"
	"syscall/js"
	"testing"
	"time"
)

func fetchTestPromise() (promise, resolve, reject js.Value) {
	executor := js.FuncOf(func(_ js.Value, args []js.Value) any { resolve, reject = args[0], args[1]; return nil })
	promise = js.Global().Get("Promise").New(executor)
	executor.Release()
	return
}
func TestFetchPreservesBytesHeadersAndHTTPFailures(t *testing.T) {
	original := js.Global().Get("fetch")
	defer js.Global().Set("fetch", original)
	for _, scenario := range []string{"ok", "http", "transport", "body"} {
		t.Run(scenario, func(t *testing.T) {
			body := js.FuncOf(func(_ js.Value, _ []js.Value) any {
				if scenario == "body" {
					return js.Global().Get("Promise").Call("reject", js.Global().Get("Error").New("body unavailable"))
				}
				array := jsutil.NewUint8ArrayFromBytes([]byte{0, 128, 255})
				return js.Global().Get("Promise").Call("resolve", array.Get("buffer"))
			})
			defer body.Release()
			fetch := js.FuncOf(func(_ js.Value, args []js.Value) any {
				options := args[1]
				if args[0].String() != "/sample" || options.Get("method").String() != "POST" || options.Get("headers").Get("Content-Type").String() != "application/json" || options.Get("headers").Get("X-Queue").String() != "1" {
					t.Error("request headers changed")
				}
				bytes := make([]byte, 2)
				js.CopyBytesToGo(bytes, options.Get("body"))
				if string(bytes) != "{}" {
					t.Error("request body changed")
				}
				if scenario == "transport" {
					return js.Global().Get("Promise").Call("reject", "offline")
				}
				status, statusText := 200, "OK"
				if scenario == "http" {
					status, statusText = 503, "Unavailable"
				}
				return js.Global().Get("Promise").Call("resolve", map[string]any{"status": status, "statusText": statusText, "arrayBuffer": body})
			})
			defer fetch.Release()
			js.Global().Set("fetch", fetch)
			response, err := Fetch(context.Background(), FetchRequest{URL: "/sample", Method: "POST", ContentType: "application/json", Headers: map[string]string{"X-Queue": "1"}, Body: []byte("{}")})
			if scenario == "transport" || scenario == "body" {
				if err == nil {
					t.Fatal("promise failure lost")
				}
				if scenario == "body" && response.Status != 200 {
					t.Fatal("body failure lost received HTTP metadata")
				}
				return
			}
			if err != nil || string(response.Body) != string([]byte{0, 128, 255}) {
				t.Fatal("response bytes changed", err)
			}
			if scenario == "http" && (response.Status != 503 || response.StatusText != "Unavailable") {
				t.Fatal("HTTP error became transport failure")
			}
		})
	}
}

func TestFetchCancellationAbortsResponseAndBodyWaits(t *testing.T) {
	original := js.Global().Get("fetch")
	defer js.Global().Set("fetch", original)
	for _, bodyPending := range []bool{false, true} {
		promise, _, reject := fetchTestPromise()
		entered := make(chan struct{}, 1)
		body := js.FuncOf(func(_ js.Value, _ []js.Value) any { entered <- struct{}{}; return promise })
		defer body.Release()
		var signal js.Value
		fetch := js.FuncOf(func(_ js.Value, args []js.Value) any {
			signal = args[1].Get("signal")
			if !bodyPending {
				entered <- struct{}{}
				return promise
			}
			return js.Global().Get("Promise").Call("resolve", map[string]any{"status": 200, "statusText": "OK", "arrayBuffer": body})
		})
		defer fetch.Release()
		js.Global().Set("fetch", fetch)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := Fetch(ctx, FetchRequest{URL: "/pending"}); done <- err }()
		<-entered
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation identity lost", err)
			}
		case <-time.After(time.Second):
			t.Fatal("fetch retained never-settling promise")
		}
		if !signal.Get("aborted").Bool() {
			t.Fatal("network operation did not receive abort")
		}
		reject.Invoke(js.Global().Get("Error").New("late network rejection"))
		_, _ = jsutil.AwaitPromise(js.Global().Get("Promise").Call("resolve", nil))
	}
}

func TestFetchInvalidContextAndUnavailableTransport(t *testing.T) {
	original := js.Global().Get("fetch")
	defer js.Global().Set("fetch", original)
	js.Global().Set("fetch", js.Undefined())
	if _, err := Fetch(context.Background(), FetchRequest{}); !errors.Is(err, ErrFetchUnavailable) {
		t.Fatal(err)
	}
	if _, err := Fetch(nil, FetchRequest{}); err == nil || !strings.Contains(err.Error(), "context required") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Fetch(ctx, FetchRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
