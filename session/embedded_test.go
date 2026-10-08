package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbeddedCookiePolicy(t *testing.T) {
	for _, opts := range []Options{
		{SameSite: http.SameSiteNoneMode, AllowInsecure: true},
		{Partitioned: true},
		{Partitioned: true, SameSite: http.SameSiteNoneMode, AllowInsecure: true},
	} {
		if _, err := New("embedded-session-secret", opts); err == nil {
			t.Errorf("accepted unsafe cookies: %+v", opts)
		}
	}
	m := MustNew("embedded-session-secret", Options{SameSite: http.SameSiteNoneMode, Partitioned: true, Path: "/.proxy/game"})
	var token string
	w := httptest.NewRecorder()
	m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { Current(r).Set("user", true); token = Token(r) })).ServeHTTP(w, httptest.NewRequest("GET", "https://activity.example/", nil))
	cookie := w.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || !cookie.Partitioned || cookie.SameSite != http.SameSiteNoneMode || cookie.Path != "/.proxy/game" || token == "" {
		t.Fatalf("cookie policy: %s", cookie)
	}
	destroy := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "https://activity.example/", nil)
	req.AddCookie(cookie)
	m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { Current(r).Destroy() })).ServeHTTP(destroy, req)
	deleted := destroy.Result().Cookies()[0]
	if !deleted.Partitioned || !deleted.Secure || deleted.SameSite != http.SameSiteNoneMode || deleted.Path != cookie.Path || deleted.MaxAge != -1 {
		t.Fatalf("deletion policy: %s", deleted)
	}
	defaults := MustNew("embedded-session-secret", Options{}).opts
	if defaults.Partitioned || !defaults.Secure || defaults.SameSite != http.SameSiteLaxMode {
		t.Fatalf("changed defaults: %+v", defaults)
	}
}

func TestEmbeddedDiscordOriginStillRequiresToken(t *testing.T) {
	const activity = "https://12345678.discordsays.com"
	m := MustNew("embedded-session-secret", Options{SameSite: http.SameSiteNoneMode, Partitioned: true, TrustedOrigins: []string{activity}})
	w := httptest.NewRecorder()
	var token string
	m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { Current(r).Set("active", true); token = Token(r) })).ServeHTTP(w, httptest.NewRequest("GET", "https://app.example/", nil))
	cookie := w.Result().Cookies()[0]
	handler := m.Middleware(m.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })))
	for _, tc := range []struct {
		origin, token string
		status        int
	}{{activity, token, 204}, {activity, "", 403}, {"https://discord.com", token, 403}, {"https://other.discordsays.com", token, 403}, {"https://evil.example", token, 403}, {"null", token, 403}} {
		req := httptest.NewRequest("POST", "https://app.example/save", nil)
		req.AddCookie(cookie)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("X-CSRF-Token", tc.token)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != tc.status {
			t.Errorf("origin=%s token=%t status=%d want=%d", tc.origin, tc.token != "", out.Code, tc.status)
		}
	}
}
