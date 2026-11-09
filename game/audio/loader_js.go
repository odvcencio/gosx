//go:build js && wasm

package audio

import (
	"fmt"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

// HTTPLoader loads clip bytes through the browser's fetch API. Unlike the
// native loader it links no net/http, so a game's wasm binary stays small.
// Call Load from a goroutine, never inside a syscall/js callback.
type HTTPLoader struct{}

// Load implements ClipLoader.
func (HTTPLoader) Load(uri string) (data []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("audio: GET %s: %v", uri, r)
		}
	}()
	fetch := js.Global().Get("fetch")
	if fetch.Type() != js.TypeFunction {
		return nil, fmt.Errorf("audio: GET %s: fetch is unavailable", uri)
	}
	resp, err := jsutil.AwaitPromise(fetch.Invoke(uri))
	if err != nil {
		return nil, fmt.Errorf("audio: GET %s: %w", uri, err)
	}
	if !resp.Get("ok").Bool() {
		return nil, fmt.Errorf("audio: GET %s: status %d", uri, resp.Get("status").Int())
	}
	buf, err := jsutil.AwaitPromise(resp.Call("arrayBuffer"))
	if err != nil {
		return nil, fmt.Errorf("audio: GET %s: %w", uri, err)
	}
	data = make([]byte, buf.Get("byteLength").Int())
	js.CopyBytesToGo(data, js.Global().Get("Uint8Array").New(buf))
	return data, nil
}
