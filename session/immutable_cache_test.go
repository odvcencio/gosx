package session

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"m31labs.dev/gosx/internal/httpcache"
)

func TestImmutableAssetClassificationFailsClosedAtCommit(t *testing.T) {
	m := MustNew("immutable-asset-test-secret", Options{})
	cookie := issueCookie(t, m, "viewer", "signed-in")
	for _, tc := range []struct {
		name     string
		classify bool
		modify   func(http.ResponseWriter, *http.Request)
		status   int
	}{
		{"unclassified-public-header", false, nil, http.StatusOK},
		{"late-cookie", true, func(w http.ResponseWriter, _ *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "preference", Value: "value"})
		}, http.StatusOK},
		{"late-session-write", true, func(_ http.ResponseWriter, r *http.Request) { Current(r).Set("updated", true) }, http.StatusOK},
		{"late-cookie-variance", true, func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Vary", "Accept-Encoding, COOKIE") }, http.StatusOK},
		{"late-session-header-variance", true, func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Vary", "X-Session") }, http.StatusOK},
		{"late-private-policy", true, func(w http.ResponseWriter, _ *http.Request) { w.Header().Add("Cache-Control", "PRIVATE=\"field\"") }, http.StatusOK},
		{"late-no-store", true, func(w http.ResponseWriter, _ *http.Request) { w.Header().Add("Cache-Control", "NO-STORE") }, http.StatusOK},
		{"late-html", true, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}, http.StatusOK},
		{"late-data", true, func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Content-Type", "application/json") }, http.StatusOK},
		{"late-json-suffix", true, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/problem+json")
		}, http.StatusOK},
		{"missing-type", true, func(w http.ResponseWriter, _ *http.Request) { w.Header().Del("Content-Type") }, http.StatusOK},
		{"error", true, nil, http.StatusInternalServerError},
		{"redirect", true, nil, http.StatusFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = Current(r).String("viewer")
				w.Header().Set("Content-Type", "application/javascript")
				w.Header().Set("Cache-Control", httpcache.Immutable)
				if tc.classify {
					httpcache.MarkImmutableAsset(w, r)
				}
				if tc.modify != nil {
					tc.modify(w, r)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("response"))
			}))
			for _, visitor := range []*http.Cookie{cookie, nil} {
				if !tc.classify && visitor == nil {
					// Anonymous session reads retain their existing policy,
					// with Cookie variance, until a visitor has session state.
					continue
				}
				r := httptest.NewRequest(http.MethodGet, "/response", nil)
				if visitor != nil {
					r.AddCookie(visitor)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if got := w.Result().Header.Get("Cache-Control"); got != httpcache.Private {
					t.Fatalf("session=%t Cache-Control=%q", visitor != nil, got)
				}
			}
		})
	}
}

func TestSetCookieIsPrivateWithoutSessionAccess(t *testing.T) {
	m := MustNew("immutable-asset-test-secret", Options{})
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", httpcache.Immutable)
		http.SetCookie(w, &http.Cookie{Name: "preference", Value: "value"})
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/response", nil))
	if got := w.Result().Header.Get("Cache-Control"); got != httpcache.Private {
		t.Fatalf("Cache-Control=%q", got)
	}
}

func TestImmutableAssetEarlyHintsUseFinalHeaders(t *testing.T) {
	m := MustNew("immutable-asset-test-secret", Options{})
	cookie := issueCookie(t, m, "viewer", "signed-in")
	for _, writeSession := range []bool{false, true} {
		h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = Current(r).String("viewer")
			w.Header().Set("Link", "</asset.js>; rel=preload")
			w.WriteHeader(http.StatusEarlyHints)
			w.Header().Set("Content-Type", "application/javascript")
			httpcache.MarkImmutableAsset(w, r)
			if writeSession {
				Current(r).Set("updated", true)
			}
			_, _ = w.Write([]byte("// asset"))
		}))
		server := httptest.NewServer(h)
		r, err := http.NewRequest(http.MethodGet, server.URL, nil)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		r.AddCookie(cookie)
		res, err := server.Client().Do(r)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		server.Close()
		want := httpcache.Immutable
		if writeSession {
			want = httpcache.Private
		}
		if res.StatusCode != http.StatusOK || res.Header.Get("Cache-Control") != want ||
			(len(res.Cookies()) != 0) != writeSession {
			t.Fatalf("write=%t status=%d headers=%v", writeSession, res.StatusCode, res.Header)
		}
	}
}
