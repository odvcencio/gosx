//go:build js && wasm

package audio

import (
	"syscall/js"
	"testing"
)

func TestHTTPLoaderUsesFetch(t *testing.T) {
	g := js.Global()
	previous := g.Get("fetch")
	t.Cleanup(func() { g.Set("fetch", previous) })
	g.Set("fetch", g.Get("Function").New("uri", `
		if (uri === "/missing") return Promise.resolve({ ok: false, status: 404 });
		return Promise.resolve({ ok: true, status: 200, arrayBuffer: () => Promise.resolve(new Uint8Array([1, 2, 3]).buffer) });`))

	data, err := HTTPLoader{}.Load("/clip.ogg")
	if err != nil || len(data) != 3 || data[0] != 1 || data[2] != 3 {
		t.Fatalf("Load = %v, %v", data, err)
	}
	if _, err := (HTTPLoader{}).Load("/missing"); err == nil {
		t.Fatal("a 404 must be an error")
	}
}
