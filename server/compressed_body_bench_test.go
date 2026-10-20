package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
)

func compressedBenchmarkBody() string {
	var b strings.Builder
	for i := 0; i < 1200; i++ {
		fmt.Fprintf(&b, `<p data-row="%d">Item %d has a reproducible shareable body.</p>`, i, i)
	}
	return b.String()
}

func BenchmarkShareableHTMLBrotliGET(b *testing.B) {
	body := compressedBenchmarkBody()
	h := CompressionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cache := NewCacheState()
		cache.SetPolicy(PublicCache(time.Minute))
		WriteHTML(w, HTMLResponse{Node: gosx.RawHTML(body), Request: r, Cache: cache, Headers: make(http.Header)})
	}))
	request := func() {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", "br")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("Content-Encoding") != "br" {
			b.Fatal(w.Code, w.Header())
		}
	}
	request()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		request()
	}
}
