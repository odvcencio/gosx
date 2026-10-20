package dev

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"m31labs.dev/gosx/session"
)

func TestDevProxyPreservesHostForCSRF(t *testing.T) {
	m := session.MustNew("dev-origin-session-secret", session.Options{})
	upstream := httptest.NewServer(m.Middleware(m.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "app.example" {
			t.Errorf("upstream Host = %q, want browser-facing host", r.Host)
		}
		w.WriteHeader(http.StatusNoContent)
	}))))
	defer upstream.Close()
	s := &Server{Dir: t.TempDir(), BuildDir: t.TempDir(), ProxyTarget: upstream.URL}
	s.SetProxyTarget(upstream.URL)
	h := s.Handler()
	for _, tc := range []struct {
		name, origin, site string
		want               int
	}{
		{"same origin", "http://app.example", "same-origin", 204},
		{"origin only", "http://app.example", "", 204},
		{"metadata only", "", "same-origin", 204},
		{"foreign origin", "https://foreign.example", "same-origin", 403},
		{"cross site", "", "cross-site", 403},
		{"missing metadata and token", "", "", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://app.example/form", nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("POST status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}
