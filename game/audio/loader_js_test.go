//go:build js && wasm

package audio

import (
	"errors"
	"strings"
	"sync/atomic"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/client/browser"
	"m31labs.dev/gosx/client/jsutil"
)

// stubOrigin is the URL prefix the fake fetch answers for. It is not a real
// host: under js/wasm there is no listening socket to dial, and Node's fetch
// refuses the placeholder address httptest reports.
const stubOrigin = "https://audio.test"

// replaceFetch installs value as the global fetch for the rest of the test.
func replaceFetch(t *testing.T, value any) {
	t.Helper()
	global := js.Global()
	previous := global.Get("fetch")
	global.Set("fetch", value)
	t.Cleanup(func() { global.Set("fetch", previous) })
}

// fakeResponse builds what the fake fetch resolves with: the fields that
// client/browser reads from a fetch Response. arrayBuffer is called with the
// response as "this" and reads the bytes from the "bytes" field.
func fakeResponse(status int, body []byte, arrayBuffer js.Func) map[string]any {
	return map[string]any{
		"status":      status,
		"statusText":  "",
		"bytes":       jsutil.NewUint8ArrayFromBytes(body).Get("buffer"),
		"arrayBuffer": arrayBuffer,
	}
}

// bodyReader returns an arrayBuffer method that resolves with the response's
// bytes, like a body that arrives in full.
func bodyReader(t *testing.T) js.Func {
	t.Helper()
	fn := js.FuncOf(func(response js.Value, _ []js.Value) any {
		return js.Global().Get("Promise").Call("resolve", response.Get("bytes"))
	})
	t.Cleanup(fn.Release)
	return fn
}

// serveStub installs a fake global fetch whose answer to each request comes
// from respond, and returns the URL prefix it answers for and a counter of the
// requests it has served. The fake rejects anything but a plain GET of a URL
// under that prefix, because that is all a clip load should send.
func serveStub(t *testing.T, respond func(path string) stubResponse) (string, func() int) {
	t.Helper()
	global := js.Global()
	var count atomic.Int64

	arrayBuffer := bodyReader(t)
	// A body that fails after the headers arrived makes arrayBuffer() reject.
	failingBody := js.FuncOf(func(js.Value, []js.Value) any {
		return global.Get("Promise").Call("reject", global.Get("Error").New("body stream aborted"))
	})
	t.Cleanup(failingBody.Release)

	fetch := js.FuncOf(func(_ js.Value, args []js.Value) any {
		count.Add(1)
		reject := func(message string) any {
			t.Errorf("fake fetch: %s", message)
			return global.Get("Promise").Call("reject", global.Get("Error").New(message))
		}
		if len(args) != 2 {
			return reject("fetch takes a URL and options")
		}
		url, options := args[0].String(), args[1]
		if method := options.Get("method").String(); method != "GET" {
			return reject("method " + method + ", want GET")
		}
		for _, name := range []string{"body", "headers"} {
			if !options.Get(name).IsUndefined() {
				return reject("a clip request sets " + name + "; it should be a bare GET")
			}
		}
		path, ok := strings.CutPrefix(url, stubOrigin)
		if !ok {
			return reject("URL " + url + " is outside " + stubOrigin)
		}

		reply := respond(path)
		if reply.drop {
			return global.Get("Promise").Call("reject", global.Get("Error").New("offline"))
		}
		status := reply.status
		if status == 0 {
			status = 200
		}
		body := arrayBuffer
		if reply.cutBody {
			body = failingBody
		}
		return global.Get("Promise").Call("resolve", fakeResponse(status, reply.body, body))
	})
	t.Cleanup(fetch.Release)
	replaceFetch(t, fetch)
	return stubOrigin, func() int { return int(count.Load()) }
}

// A page without fetch cannot load clips. The error keeps the cause that
// client/browser reports, so callers can still match it with errors.Is.
func TestHTTPLoaderReportsMissingFetch(t *testing.T) {
	replaceFetch(t, js.Undefined())

	got, err := (HTTPLoader{}).Load(stubOrigin + "/boom.wav")
	if !errors.Is(err, browser.ErrFetchUnavailable) {
		t.Fatalf("Load error = %v, want one that wraps browser.ErrFetchUnavailable", err)
	}
	if want := "audio: GET " + stubOrigin + "/boom.wav: "; !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("Load error = %q, want it to start with %q", err, want)
	}
	if got != nil {
		t.Fatalf("Load returned % x with an error, want no bytes", got)
	}
}

// A 200 whose body fails is a read error, not a status error, and it keeps the
// cause that client/browser reports. The shared test checks only that it fails.
func TestHTTPLoaderBodyFailureWrapsCause(t *testing.T) {
	base, _ := serveStub(t, func(string) stubResponse {
		return stubResponse{body: []byte("RIFF"), cutBody: true}
	})
	uri := base + "/cut.wav"

	got, err := (HTTPLoader{}).Load(uri)
	if err == nil {
		t.Fatal("Load succeeded, want an error for a body that failed after the status arrived")
	}
	if want := "audio: GET " + uri + ": "; !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("Load error = %q, want it to start with %q", err, want)
	}
	if !strings.Contains(err.Error(), "body stream aborted") {
		t.Fatalf("Load error = %q, want it to carry the body failure", err)
	}
	if strings.Contains(err.Error(), "status") {
		t.Fatalf("Load error = %q, want a read error, not a status error", err)
	}
	if got != nil {
		t.Fatalf("Load returned % x with an error, want no bytes", got)
	}
}

// fetch reports status 0 and no error for an opaque response, which a plain
// GET does not get today. Status 0 is still not a 200: the loader must reject
// it with the status text, not return the empty body as a clip. This pins the
// reason Load tests the error as well as the status when it sees status 0.
func TestHTTPLoaderRejectsStatusZeroWithoutError(t *testing.T) {
	global := js.Global()
	arrayBuffer := bodyReader(t)
	fetch := js.FuncOf(func(js.Value, []js.Value) any {
		return global.Get("Promise").Call("resolve", fakeResponse(0, nil, arrayBuffer))
	})
	t.Cleanup(fetch.Release)
	replaceFetch(t, fetch)

	uri := stubOrigin + "/opaque.wav"
	got, err := (HTTPLoader{}).Load(uri)
	if want := "audio: GET " + uri + ": status 0"; err == nil || err.Error() != want {
		t.Fatalf("Load error = %v, want %q", err, want)
	}
	if got != nil {
		t.Fatalf("Load returned % x with an error, want no bytes", got)
	}
}
