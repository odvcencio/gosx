package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIServerErrorsHideDetailsAndIncludeRequestID(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler APIHandler
		status  int
	}{
		{"panic", func(*Context) (any, error) { panic("private error detail") }, 500},
		{"error", func(*Context) (any, error) { return nil, errors.New("private error detail") }, 500},
		{"unavailable", func(ctx *Context) (any, error) { ctx.SetStatus(503); return nil, errors.New("private error detail") }, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := New()
			app.API("/api/error", tc.handler)
			r := httptest.NewRequest("GET", "/api/error", nil)
			r.Header.Set("X-Request-ID", "test-request")
			w := httptest.NewRecorder()
			app.Build().ServeHTTP(w, r)
			if w.Code != tc.status || strings.Contains(w.Body.String(), "private error detail") || !strings.Contains(w.Body.String(), http.StatusText(tc.status)) || !strings.Contains(w.Body.String(), "test-request") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestReadinessHidesDetailsAndIncludesRequestID(t *testing.T) {
	app := New()
	app.UseReadyCheck("dependency", ReadyCheckFunc(func(context.Context) error { return errors.New("private dependency detail") }))
	r := httptest.NewRequest("GET", "/readyz", nil)
	r.Header.Set("X-Request-ID", "ready-request")
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "private dependency detail") || !strings.Contains(w.Body.String(), "ready-request") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDefaultFrameAncestorsAndExplicitEmbeddingPolicy(t *testing.T) {
	for _, policy := range []SecurityPolicy{{}, {ContentSecurityPolicy: "frame-ancestors https://embed.example"}} {
		w := httptest.NewRecorder()
		securityHeadersMiddleware(policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		want := "frame-ancestors 'self'"
		if policy.ContentSecurityPolicy != "" {
			want = policy.ContentSecurityPolicy
		}
		if got := w.Header().Get("Content-Security-Policy"); got != want {
			t.Fatalf("CSP=%q want=%q", got, want)
		}
	}
}
