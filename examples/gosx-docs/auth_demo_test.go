package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx/server"
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
