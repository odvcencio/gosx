//go:build !js || !wasm

package worklet

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerServesVersionedProcessor(t *testing.T) {
	h := Handler()
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/"+Path(), nil))
	if r.Code != http.StatusOK || !bytes.Equal(r.Body.Bytes(), Source()) || r.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset response %d %v", r.Code, r.Header())
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req := httptest.NewRequest(method, "/"+Path(), nil)
		req.Header.Set("If-None-Match", r.Header().Get("ETag"))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
			t.Fatalf("%s conditional response %d", method, rec.Code)
		}
	}
	for _, name := range []string{"processor.js", "worklet/wasm-processor.stale.js", "../worklet.go"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+name, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("unexpected asset %s: %d", name, rec.Code)
		}
	}
}

func TestHandlerRejectsOtherMethods(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, httptest.NewRequest(method, "/"+Path(), nil))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("%s response %d, Allow %q; want 405 with GET, HEAD", method, rec.Code, rec.Header().Get("Allow"))
		}
	}
}
