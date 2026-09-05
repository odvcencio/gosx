package server

import (
	"io"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	"net/http"
)

func serveServerSlotRuntime(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("v") != runtimehost.ServerSlotRuntimeHash() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	MarkObservedRequest(r, "runtime", "/gosx/server-slots.js")
	io.WriteString(w, runtimehost.ServerSlotRuntime())
}
