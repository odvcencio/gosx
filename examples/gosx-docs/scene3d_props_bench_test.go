package main

import (
	"net/http"
	"net/http/httptest"

	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/localapp"
	"m31labs.dev/gosx/server"
)

func benchSceneDocsHandler(b *testing.B) http.Handler {
	b.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(thisFile)
	env, err := localapp.Environment(nil, "8080")
	if err != nil {
		b.Fatal(err)
	}
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "SESSION_SECRET="); ok {
			b.Setenv("SESSION_SECRET", value)
		}
	}
	app, err := buildDocsApp(root, "8080")
	if err != nil {
		b.Fatal(err)
	}
	return app.Build()
}

func benchSceneDocsRequest(b *testing.B, path, enc string) {
	h := benchSceneDocsHandler(b)
	req := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if enc != "" {
			r.Header.Set("Accept-Encoding", enc)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := req()
	if w.Code != 200 {
		b.Fatalf("status %d for %s", w.Code, path)
	}
	wire := float64(w.Body.Len())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if w := req(); w.Code != 200 {
			b.Fatal(w.Code)
		}
	}
	b.StopTimer()
	b.ReportMetric(wire, "body_bytes")
}

func BenchmarkDocsScene3DRoutes(b *testing.B) {
	for _, path := range []string{"/demos/water/", "/demos/beacon/"} {
		b.Run(path, func(b *testing.B) { benchSceneDocsRequest(b, path, "") })
	}
}
