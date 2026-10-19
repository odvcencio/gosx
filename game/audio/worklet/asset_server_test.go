//go:build !js || !wasm

package worklet

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProcessorAsset(t *testing.T) {
	data := Source()
	if len(data) > 24*1024 {
		t.Fatalf("processor exceeds 24 KiB raw budget: %d", len(data))
	}
	var compressed bytes.Buffer
	z := gzip.NewWriter(&compressed)
	_, _ = z.Write(data)
	_ = z.Close()
	if compressed.Len() > 6*1024 {
		t.Fatalf("processor exceeds 6 KiB gzip budget: %d", compressed.Len())
	}
	data[0] ^= 1
	if bytes.Equal(data, Source()) {
		t.Fatal("caller can mutate the shared processor")
	}
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

func TestProcessorPathAndETagMatchSourceDigest(t *testing.T) {
	digest := fmt.Sprintf("%x", sha256.Sum256(Source()))
	if want := "worklet/wasm-processor." + digest[:16] + ".js"; Path() != want {
		t.Fatalf("processor path %q, want %q", Path(), want)
	}
	r := httptest.NewRecorder()
	Handler().ServeHTTP(r, httptest.NewRequest(http.MethodHead, "/"+Path(), nil))
	if r.Code != http.StatusOK || r.Body.Len() != 0 || r.Header().Get("ETag") != `"`+digest+`"` {
		t.Fatalf("HEAD response %d %v body=%d", r.Code, r.Header(), r.Body.Len())
	}
}
