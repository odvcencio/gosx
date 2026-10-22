package server

import (
	"net/http"
	"strconv"
	"strings"

	"m31labs.dev/gosx/internal/httpcache"
	"m31labs.dev/gosx/island"
)

// serveSceneEntry serves the props of a deferred Scene3D by content hash.
// The URL is immutable: a different scene has a different hash.
//
// It uses httpcache.MarkImmutableContentAddressedJSON rather than
// MarkImmutableAsset: the latter accepts only script, style, font and image
// content types, and the session writer forces "private" at commit for an
// asset it marked but could not certify (session/session.go commitCookie).
// Without the mark, a global middleware that reads the session would make
// this response private.
func (a *App) serveSceneEntry(w http.ResponseWriter, r *http.Request, file string) {
	hash, ok := strings.CutSuffix(file, ".json")
	store := island.DefaultSceneEntryStore()
	if !ok || store == nil || !island.ValidSceneEntryHash(hash) {
		http.NotFound(w, r)
		return
	}
	body, ok := store.Get(hash)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("ETag", `"`+hash+`"`)
	httpcache.MarkImmutableContentAddressedJSON(w, r)
	MarkObservedRequest(r, "runtime", island.SceneEntryPath(hash))
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		return
	}
	_, _ = w.Write(body)
}
