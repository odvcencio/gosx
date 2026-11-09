//go:build !js || !wasm

package audio

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPLoaderNonOKStatusErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	if _, err := (HTTPLoader{}).Load(server.URL); err == nil {
		t.Fatal("expected a non-200 status to error")
	}
}
