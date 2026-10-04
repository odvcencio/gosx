package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// TestDocsMagicLinkDemoDeliversWithoutExposingLink covers the local auth
// demo after magic links began requiring a sender: the request must succeed
// (a redirect, not a 500) and must never return the sign-in link or token.
func TestDocsMagicLinkDemoDeliversWithoutExposingLink(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(thisFile)
	configureDocsTestSecret(t)
	app, err := buildDocsApp(root, "8080")
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()

	form := url.Values{"email": {"reader@example.com"}, "next": {"/docs/auth"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/request", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://"+req.Host)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("magic-link request status = %d, want %d; body=%q", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	for name, value := range map[string]string{"Location": rec.Header().Get("Location"), "body": rec.Body.String()} {
		if strings.Contains(value, "token=") {
			t.Fatalf("magic-link response %s exposes the sign-in link: %q", name, value)
		}
	}
}

func TestDocsMagicLinkHintFlashStates(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	configureDocsTestSecret(t)
	app, err := buildDocsApp(server.ResolveAppRoot(thisFile), "8080")
	if err != nil {
		t.Fatal(err)
	}
	var status string
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/__test/flash" {
				if status != "" && !session.AddFlash(r, "magicLink", map[string]any{"status": status, "email": "reader@example.com"}) {
					t.Fatal("session flash was not installed")
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	handler := app.Build()
	for _, state := range []string{"", "sent", "error", "signed_in"} {
		name := state
		if name == "" {
			name = "absent"
		}
		t.Run(name, func(t *testing.T) {
			status = state
			rec := httptest.NewRecorder()
			seed := httptest.NewRecorder()
			handler.ServeHTTP(seed, httptest.NewRequest(http.MethodGet, "/__test/flash", nil))
			req := httptest.NewRequest(http.MethodGet, "/docs/auth", nil)
			for _, cookie := range seed.Result().Cookies() {
				req.AddCookie(cookie)
			}
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("auth page status = %d", rec.Code)
			}
			body := rec.Body.String()
			if got, want := strings.Contains(body, "Open the sign-in link printed in the server log."), state == "sent"; got != want {
				t.Fatalf("flash %q renders sign-in hint = %t, want %t", state, got, want)
			}
			if got, want := strings.Contains(body, "Magic-link status"), state != ""; got != want {
				t.Fatalf("flash %q renders status callout = %t, want %t", state, got, want)
			}
		})
	}
}
