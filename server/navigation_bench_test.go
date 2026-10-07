package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"m31labs.dev/gosx"
)

// BenchmarkNavigationPage measures HTML assembly for a small page with the
// navigation runtime enabled. It excludes network transfer and compression.
func BenchmarkNavigationPage(b *testing.B) {
	app := New()
	app.EnableNavigation()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		return gosx.El("main", gosx.El("h1", gosx.Text("Navigation benchmark")))
	})
	handler := app.Build()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	var htmlBytes int
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			b.Fatalf("status %d", response.Code)
		}
		htmlBytes = response.Body.Len()
	}
	b.ReportMetric(float64(htmlBytes), "html_bytes")
}
