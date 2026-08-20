package session

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// setHandler stores one session value so the manager writes a cookie.
func setHandler(manager *Manager, key, value string) http.Handler {
	return manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Current(r).Set(key, value)
		w.WriteHeader(http.StatusNoContent)
	}))
}

func issueCookie(t *testing.T, manager *Manager, key, value string) *http.Cookie {
	t.Helper()
	res := httptest.NewRecorder()
	setHandler(manager, key, value).ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/set", nil))
	cookies := res.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected a session cookie")
	}
	return cookies[0]
}

func readSessionValue(t *testing.T, manager *Manager, cookie *http.Cookie, key string) (string, *httptest.ResponseRecorder) {
	t.Helper()
	var got string
	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = Current(r).String(key)
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/read", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return got, res
}

// TestSessionCookieExpiresServerSide proves the manager rejects a replayed
// cookie after the configured max age. Before the timestamp fix the cookie
// replayed forever, because only the browser attributes carried the max age.
func TestSessionCookieExpiresServerSide(t *testing.T) {
	for _, encrypt := range []bool{false, true} {
		name := "signed"
		if encrypt {
			name = "encrypted"
		}
		t.Run(name, func(t *testing.T) {
			manager := MustNew("session-expiry-secret-value", Options{
				MaxAge:  400 * time.Millisecond,
				Encrypt: encrypt,
			})
			cookie := issueCookie(t, manager, "team", "platform")

			if got, _ := readSessionValue(t, manager, cookie, "team"); got != "platform" {
				t.Fatalf("fresh cookie value = %q, want platform", got)
			}

			time.Sleep(900 * time.Millisecond)

			if got, _ := readSessionValue(t, manager, cookie, "team"); got != "" {
				t.Fatalf("expired cookie replayed value %q", got)
			}
		})
	}
}

// TestSessionCookieRejectsExpiredEnvelopeWithFixedClock pins the age check
// with an injected clock, so the result does not depend on wall-clock timing.
func TestSessionCookieRejectsExpiredEnvelopeWithFixedClock(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	manager := MustNew("session-clock-secret-value", Options{MaxAge: time.Hour})
	manager.now = func() time.Time { return base }

	cookie := issueCookie(t, manager, "team", "platform")

	manager.now = func() time.Time { return base.Add(30 * time.Minute) }
	if got, _ := readSessionValue(t, manager, cookie, "team"); got != "platform" {
		t.Fatalf("cookie inside max age = %q, want platform", got)
	}

	manager.now = func() time.Time { return base.Add(2 * time.Hour) }
	if got, _ := readSessionValue(t, manager, cookie, "team"); got != "" {
		t.Fatalf("cookie past max age replayed value %q", got)
	}
}

// TestSessionCookieRenewsPastHalfLife proves an active session gets a fresh
// cookie once it passes half of its lifetime.
func TestSessionCookieRenewsPastHalfLife(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	manager := MustNew("session-renew-secret-value", Options{MaxAge: time.Hour})
	manager.now = func() time.Time { return base }
	cookie := issueCookie(t, manager, "team", "platform")

	manager.now = func() time.Time { return base.Add(10 * time.Minute) }
	_, early := readSessionValue(t, manager, cookie, "team")
	if len(early.Result().Cookies()) != 0 {
		t.Fatal("expected no cookie rewrite inside the first half of the lifetime")
	}

	manager.now = func() time.Time { return base.Add(40 * time.Minute) }
	_, late := readSessionValue(t, manager, cookie, "team")
	renewed := late.Result().Cookies()
	if len(renewed) == 0 {
		t.Fatal("expected a renewed cookie past half of the lifetime")
	}

	manager.now = func() time.Time { return base.Add(90 * time.Minute) }
	if got, _ := readSessionValue(t, manager, renewed[0], "team"); got != "platform" {
		t.Fatalf("renewed cookie value = %q, want platform", got)
	}
}

// TestSessionCookieRejectsFutureTimestamp proves a stamp beyond the skew
// tolerance fails closed.
func TestSessionCookieRejectsFutureTimestamp(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	manager := MustNew("session-future-secret-value", Options{MaxAge: time.Hour})
	manager.now = func() time.Time { return base.Add(24 * time.Hour) }
	cookie := issueCookie(t, manager, "team", "platform")

	manager.now = func() time.Time { return base }
	if got, _ := readSessionValue(t, manager, cookie, "team"); got != "" {
		t.Fatalf("future cookie replayed value %q", got)
	}
}

// TestSessionLegacyCookieGrace covers the compatibility policy for a cookie
// that carries no issuance timestamp.
func TestSessionLegacyCookieGrace(t *testing.T) {
	unstamped := func(t *testing.T, manager *Manager) string {
		t.Helper()
		payload := []byte(`{"values":{"team":"platform"}}`)
		body := encodeUnstampedCookie(t, manager, payload)
		return body
	}

	t.Run("accepted inside the grace window and re-issued", func(t *testing.T) {
		manager := MustNew("session-legacy-secret-value", Options{})
		cookie := &http.Cookie{Name: manager.opts.CookieName, Value: unstamped(t, manager)}
		got, res := readSessionValue(t, manager, cookie, "team")
		if got != "platform" {
			t.Fatalf("legacy cookie value = %q, want platform", got)
		}
		cookies := res.Result().Cookies()
		if len(cookies) == 0 {
			t.Fatal("expected the legacy cookie to be re-issued with a timestamp")
		}
		envelope, err := manager.decode(cookies[0].Value)
		if err != nil {
			t.Fatalf("decode re-issued cookie: %v", err)
		}
		if envelope.IssuedAt == 0 {
			t.Fatal("re-issued cookie carries no timestamp")
		}
	})

	t.Run("rejected after the grace window", func(t *testing.T) {
		manager := MustNew("session-legacy-secret-value", Options{
			LegacyCookieGrace: time.Minute,
		})
		cookie := &http.Cookie{Name: manager.opts.CookieName, Value: unstamped(t, manager)}
		manager.now = func() time.Time { return manager.startedAt.Add(2 * time.Minute) }
		if got, _ := readSessionValue(t, manager, cookie, "team"); got != "" {
			t.Fatalf("legacy cookie past the grace window replayed value %q", got)
		}
	})

	t.Run("rejected at once with a negative grace", func(t *testing.T) {
		manager := MustNew("session-legacy-secret-value", Options{
			LegacyCookieGrace: -1,
		})
		cookie := &http.Cookie{Name: manager.opts.CookieName, Value: unstamped(t, manager)}
		if got, _ := readSessionValue(t, manager, cookie, "team"); got != "" {
			t.Fatalf("legacy cookie replayed value %q with a negative grace", got)
		}
	})
}

// TestSessionCookieSecureByDefault proves session.Options{} produces a Secure
// cookie. Before the fix the default cookie travelled over plain HTTP.
func TestSessionCookieSecureByDefault(t *testing.T) {
	manager := MustNew("session-secure-secret-value", Options{})
	cookie := issueCookie(t, manager, "team", "platform")
	if !cookie.Secure {
		t.Fatal("default session cookie is not Secure")
	}
	if !cookie.HttpOnly {
		t.Fatal("default session cookie is not HttpOnly")
	}
}

// TestSessionCookieAllowInsecureOptOut proves the documented opt-out for
// local HTTP development.
func TestSessionCookieAllowInsecureOptOut(t *testing.T) {
	manager := MustNew("session-insecure-secret-value", Options{AllowInsecure: true})
	cookie := issueCookie(t, manager, "team", "platform")
	if cookie.Secure {
		t.Fatal("AllowInsecure did not clear the Secure flag")
	}
}

// TestSessionHostPrefixRequiresSecureRootPath proves the __Host- name prefix
// rules fail closed.
func TestSessionHostPrefixRequiresSecureRootPath(t *testing.T) {
	if _, err := New("session-host-prefix-secret", Options{
		CookieName: "__Host-gosx_session",
	}); err != nil {
		t.Fatalf("__Host- cookie with secure defaults: %v", err)
	}
	if _, err := New("session-host-prefix-secret", Options{
		CookieName:    "__Host-gosx_session",
		AllowInsecure: true,
	}); err == nil {
		t.Fatal("expected an error for an insecure __Host- cookie")
	}
	if _, err := New("session-host-prefix-secret", Options{
		CookieName: "__Host-gosx_session",
		Domain:     "example.com",
	}); err == nil {
		t.Fatal("expected an error for a __Host- cookie with a domain")
	}
	if _, err := New("session-host-prefix-secret", Options{
		CookieName: "__Host-gosx_session",
		Path:       "/app",
	}); err == nil {
		t.Fatal("expected an error for a __Host- cookie outside the root path")
	}
}

// TestSessionOversizedCookieFailsClosed proves an oversized session reports a
// clear error and replaces the pending success response instead of writing a
// cookie the browser drops.
func TestSessionOversizedCookieFailsClosed(t *testing.T) {
	var reported []error
	manager := MustNew("session-size-secret-value", Options{
		OnError: func(err error) { reported = append(reported, err) },
	})

	var storeErr error
	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store := Current(r)
		store.Set("blob", strings.Repeat("a", 8192))
		w.WriteHeader(http.StatusNoContent)
		storeErr = store.Err()
	}))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/set", nil))

	if cookies := res.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("expected no cookie for an oversized session, got %d", len(cookies))
	}
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", res.Code)
	}
	if res.Body.String() != sessionFailureBody {
		t.Fatalf("body = %q, want generic terminal failure", res.Body.String())
	}
	if storeErr == nil {
		t.Fatal("expected Store.Err to report the oversized session")
	}
	if len(reported) != 1 {
		t.Fatalf("expected one reported error, got %#v", reported)
	}
}

// encodeUnstampedCookie signs a payload that carries no issuance timestamp. It
// reproduces a cookie written by a GoSX release before the timestamp fix.
func encodeUnstampedCookie(t *testing.T, manager *Manager, payload []byte) string {
	t.Helper()
	body := base64.RawURLEncoding.EncodeToString(payload)
	signature := sessionSignature(manager.secret, payload)
	return body + "." + base64.RawURLEncoding.EncodeToString(signature)
}
