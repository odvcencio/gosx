package server

import (
	"bytes"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/andybalholm/brotli"
)

func cachedCompressionResponse(cache *compressedBodyCache, body, policy, etag, nonce string, flush bool, encodings ...string) *httptest.ResponseRecorder {
	encoding := "br"
	if len(encodings) > 0 {
		encoding = encodings[0]
	}
	rec := httptest.NewRecorder()
	w := &compressionWriter{ResponseWriter: rec, encoding: encoding, bodyCache: cache, shareRequest: true}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", policy)
	w.Header().Set("ETag", etag)
	if nonce != "" {
		w.Header().Set("Content-Security-Policy", "script-src 'nonce-"+nonce+"'")
	}
	w.WriteHeader(200)
	midpoint := len(body) / 2
	_, _ = w.Write([]byte(body[:midpoint]))
	if flush {
		(&compressionFlushWriter{w}).Flush()
	}
	_, _ = w.Write([]byte(body[midpoint:]))
	w.finish()
	return rec
}

func TestCompressedBodyCacheWireIdentityAndExactBody(t *testing.T) {
	cache := newCompressedBodyCache(1<<20, 8)
	body := compressedBenchmarkBody()
	// A private response follows the original incremental encoder.
	baseline := cachedCompressionResponse(cache, body, "private", `"body"`, "", false)
	first := cachedCompressionResponse(cache, body, "public, max-age=60", `"body"`, "", false)
	second := cachedCompressionResponse(cache, body, "public, max-age=60", `"body"`, "", false)
	if !bytes.Equal(baseline.Body.Bytes(), first.Body.Bytes()) || !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatal("compressed wire bytes changed")
	}
	if first.Header().Get("ETag") != `W/"body"` || second.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatal("cache changed validators or Vary")
	}
	if cache.order.Len() != 1 {
		t.Fatal("shareable body not retained")
	}
	changed := strings.Replace(body, "Item 1 ", "Item 9 ", 1)
	next := cachedCompressionResponse(cache, changed, "public, max-age=60", `"body"`, "", false)
	decoded, err := io.ReadAll(brotli.NewReader(next.Body))
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != changed {
		t.Fatal("same ETag and length replayed a different body")
	}
}

func TestCompressedBodyCacheExcludesPrivateNonceAndStreams(t *testing.T) {
	for _, tc := range []struct {
		name, policy, etag, nonce, body string
		flush                           bool
	}{
		{"private", "private", `"body"`, "", compressedBenchmarkBody(), false},
		{"no-store", "public, no-store", `"body"`, "", compressedBenchmarkBody(), false},
		{"weak", "public", `W/"body"`, "", compressedBenchmarkBody(), false},
		{"nonce header", "public", `"body"`, "nonce", compressedBenchmarkBody(), false},
		{"nonce body", "public", `"body"`, "", `<script NONCE = 'x'></script>` + compressedBenchmarkBody(), false},
		{"stream", "public", `"body"`, "", compressedBenchmarkBody(), true},
		{"oversize", "public", `"body"`, "", strings.Repeat("a", maxCachedHTMLBody+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := newCompressedBodyCache(1<<20, 8)
			cachedCompressionResponse(cache, tc.body, tc.policy, tc.etag, tc.nonce, tc.flush)
			if cache.order.Len() != 0 {
				t.Fatal("ineligible response cached")
			}
		})
	}
}

func TestCompressedBodyCacheBoundsAndLRU(t *testing.T) {
	cache := newCompressedBodyCache(1024, 2)
	digest := sha256.Sum256([]byte("source"))
	a, b, c := compressedBodyKey{etag: `"a"`}, compressedBodyKey{etag: `"b"`}, compressedBodyKey{etag: `"c"`}
	cache.put(a, digest, []byte("a"))
	cache.put(b, digest, []byte("b"))
	cache.get(a, digest)
	cache.put(c, digest, []byte("c"))
	if _, ok := cache.get(b, digest); ok {
		t.Fatal("LRU entry retained")
	}
	cache.put(c, digest, bytes.Repeat([]byte("c"), 700))
	if cache.bytes > 1024 || cache.order.Len() > 2 {
		t.Fatal("cache exceeded bounds")
	}
	cache.put(a, digest, make([]byte, 1025))
	if _, ok := cache.get(a, digest); ok {
		t.Fatal("oversized entry retained")
	}
}

func TestCompressedBodyCacheGzipWireIdentity(t *testing.T) {
	cache := newCompressedBodyCache(1<<20, 8)
	body := compressedBenchmarkBody()
	baseline := cachedCompressionResponse(cache, body, "private", `"body"`, "", false, "gzip")
	first := cachedCompressionResponse(cache, body, "public", `"body"`, "", false, "gzip")
	second := cachedCompressionResponse(cache, body, "public", `"body"`, "", false, "gzip")
	if !bytes.Equal(baseline.Body.Bytes(), first.Body.Bytes()) || !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatal("gzip wire bytes changed")
	}
}

func TestCompressedBodyCacheRequiresAnonymousGETAnd200(t *testing.T) {
	for _, tc := range []struct {
		name, method, cookie, auth, setCookie string
		status                                int
	}{
		{name: "POST", method: "POST", status: 200}, {name: "cookie", method: "GET", cookie: "id=1", status: 200}, {name: "auth", method: "GET", auth: "Bearer token", status: 200}, {name: "Set-Cookie", method: "GET", setCookie: "id=1", status: 200}, {name: "error", method: "GET", status: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cache *compressedBodyCache
			h := CompressionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cache = w.(*compressionFlushWriter).bodyCache
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("ETag", `"body"`)
				w.Header().Set("Cache-Control", "public")
				if tc.setCookie != "" {
					w.Header().Set("Set-Cookie", tc.setCookie)
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, compressedBenchmarkBody())
			}))
			r := httptest.NewRequest(tc.method, "/", nil)
			r.Header.Set("Accept-Encoding", "br")
			r.Header.Set("Cookie", tc.cookie)
			r.Header.Set("Authorization", tc.auth)
			h.ServeHTTP(httptest.NewRecorder(), r)
			if cache.order.Len() != 0 {
				t.Fatal("response cached")
			}
		})
	}
}

func TestCompressedBodyCacheConcurrent(t *testing.T) {
	cache := newCompressedBodyCache(1<<20, 8)
	body := compressedBenchmarkBody()
	want := cachedCompressionResponse(cache, body, "public", `"body"`, "", false).Body.Bytes()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				got := cachedCompressionResponse(cache, body, "public", `"body"`, "", false).Body.Bytes()
				if !bytes.Equal(got, want) {
					t.Error("concurrent wire output changed")
				}
			}
		}()
	}
	wg.Wait()
}

func TestDeferredHTMLDisablesCompressedBodyCacheWithoutFlusher(t *testing.T) {
	cache := newCompressedBodyCache(1<<20, 8)
	writer := &compressionWriter{ResponseWriter: httptest.NewRecorder(), encoding: "br", shareRequest: true, bodyCache: cache}
	disableCompressedBodyCache(writer)
	writer.Header().Set("Content-Type", "text/html")
	writer.Header().Set("Cache-Control", "public")
	writer.Header().Set("ETag", `"body"`)
	writer.WriteHeader(200)
	io.WriteString(writer, compressedBenchmarkBody())
	writer.finish()
	if cache.order.Len() != 0 {
		t.Fatal("deferred response retained without a flusher")
	}
}
