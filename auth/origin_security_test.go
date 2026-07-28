package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx/session"
)

// issueMagicLink runs Issue inside the session middleware and returns the
// delivery and the error.
func issueMagicLink(t *testing.T, magic *MagicLinks, req *http.Request) (MagicLinkDelivery, error) {
	t.Helper()
	sessions := session.MustNew("magic-link-origin-secret", session.Options{})
	var (
		delivery MagicLinkDelivery
		issueErr error
	)
	handler := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivery, issueErr = magic.Issue(r, "ada@example.com", "/admin")
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	return delivery, issueErr
}

// TestMagicLinkIgnoresForwardedHostByDefault proves a forged X-Forwarded-Host
// cannot move the sign-in token to another host. Before the fix the link
// pointed at the attacker host.
func TestMagicLinkIgnoresForwardedHostByDefault(t *testing.T) {
	authn := New(session.MustNew("magic-link-origin-secret", session.Options{}), Options{})
	magic := authn.MagicLinks(MagicLinkOptions{BaseURL: "https://app.example"})

	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/request", nil)
	req.Host = "attacker.example"
	req.Header.Set("X-Forwarded-Host", "attacker.example")
	req.Header.Set("X-Forwarded-Proto", "https")

	delivery, err := issueMagicLink(t, magic, req)
	if err != nil {
		t.Fatalf("issue magic link: %v", err)
	}
	if !strings.HasPrefix(delivery.URL, "https://app.example/") {
		t.Fatalf("magic link url = %q, want the configured base url", delivery.URL)
	}
	if strings.Contains(delivery.URL, "attacker.example") {
		t.Fatalf("magic link url leaked the attacker host: %q", delivery.URL)
	}
}

// TestMagicLinkRequiresBaseURL proves a token-bearing flow fails closed when
// the application configures no origin.
func TestMagicLinkRequiresBaseURL(t *testing.T) {
	authn := New(session.MustNew("magic-link-origin-secret", session.Options{}), Options{})
	magic := authn.MagicLinks(MagicLinkOptions{})

	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/request", nil)
	req.Host = "attacker.example"
	req.Header.Set("X-Forwarded-Host", "attacker.example")
	req.Header.Set("X-Forwarded-Proto", "https")

	delivery, err := issueMagicLink(t, magic, req)
	if strings.Contains(delivery.URL, "attacker.example") {
		t.Fatalf("magic link url leaked the forged host: %q", delivery.URL)
	}
	if !errors.Is(err, ErrOriginNotConfigured) {
		t.Fatalf("issue error = %v, want ErrOriginNotConfigured", err)
	}
}

// TestMagicLinkAllowedHostsRejectForgedHost proves the host allowlist accepts a
// listed host and rejects every other host.
func TestMagicLinkAllowedHostsRejectForgedHost(t *testing.T) {
	authn := New(session.MustNew("magic-link-origin-secret", session.Options{}), Options{})
	magic := authn.MagicLinks(MagicLinkOptions{
		AllowedHosts: []string{"app.example"},
	})

	allowed := httptest.NewRequest(http.MethodPost, "/auth/magic-link/request", nil)
	allowed.Host = "app.example"
	delivery, err := issueMagicLink(t, magic, allowed)
	if err != nil {
		t.Fatalf("issue for an allowed host: %v", err)
	}
	if !strings.HasPrefix(delivery.URL, "http://app.example/") {
		t.Fatalf("magic link url = %q, want the allowed host", delivery.URL)
	}

	forged := httptest.NewRequest(http.MethodPost, "/auth/magic-link/request", nil)
	forged.Host = "attacker.example"
	if _, err := issueMagicLink(t, magic, forged); !errors.Is(err, ErrOriginNotAllowed) {
		t.Fatalf("issue error = %v, want ErrOriginNotAllowed", err)
	}
}

// TestMagicLinkForwardedTrustNeedsBothProxyAndAllowlist proves the forwarded
// headers count only for a request from a listed proxy.
func TestMagicLinkForwardedTrustNeedsBothProxyAndAllowlist(t *testing.T) {
	authn := New(session.MustNew("magic-link-origin-secret", session.Options{}), Options{})
	magic := authn.MagicLinks(MagicLinkOptions{
		AllowedHosts: []string{"app.example"},
		ForwardedTrust: ForwardedTrust{
			Enabled: true,
			Proxies: []string{"10.0.0.0/8"},
		},
	})

	trusted := httptest.NewRequest(http.MethodPost, "/auth/magic-link/request", nil)
	trusted.RemoteAddr = "10.1.2.3:5555"
	trusted.Host = "internal.lb"
	trusted.Header.Set("X-Forwarded-Host", "app.example")
	trusted.Header.Set("X-Forwarded-Proto", "https")
	delivery, err := issueMagicLink(t, magic, trusted)
	if err != nil {
		t.Fatalf("issue behind a trusted proxy: %v", err)
	}
	if !strings.HasPrefix(delivery.URL, "https://app.example/") {
		t.Fatalf("magic link url = %q, want the forwarded host", delivery.URL)
	}

	untrusted := httptest.NewRequest(http.MethodPost, "/auth/magic-link/request", nil)
	untrusted.RemoteAddr = "203.0.113.7:5555"
	untrusted.Host = "app.example"
	untrusted.Header.Set("X-Forwarded-Host", "attacker.example")
	untrusted.Header.Set("X-Forwarded-Proto", "https")
	delivery, err = issueMagicLink(t, magic, untrusted)
	if err != nil {
		t.Fatalf("issue from an untrusted peer: %v", err)
	}
	if strings.Contains(delivery.URL, "attacker.example") {
		t.Fatalf("untrusted forwarded host reached the link: %q", delivery.URL)
	}
}

// TestMagicLinkForwardedTrustRequiresProxyList proves the configuration fails
// closed when it enables forwarded headers without naming a proxy.
func TestMagicLinkForwardedTrustRequiresProxyList(t *testing.T) {
	authn := New(session.MustNew("magic-link-origin-secret", session.Options{}), Options{})
	magic := authn.MagicLinks(MagicLinkOptions{
		AllowedHosts:   []string{"app.example"},
		ForwardedTrust: ForwardedTrust{Enabled: true},
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/request", nil)
	req.Host = "app.example"
	if _, err := issueMagicLink(t, magic, req); err == nil {
		t.Fatal("expected a configuration error for empty ForwardedTrust.Proxies")
	}
}

// TestMagicLinkRequestHandlerReportsConfigurationFault proves the HTTP handler
// reports a server fault, not a client fault, for a missing origin.
func TestMagicLinkRequestHandlerReportsConfigurationFault(t *testing.T) {
	sessions := session.MustNew("magic-link-origin-secret", session.Options{})
	authn := New(sessions, Options{})
	magic := authn.MagicLinks(MagicLinkOptions{
		Resolver: MagicLinkResolverFunc(func(_ context.Context, email string) (User, error) {
			return User{ID: email, Email: email}, nil
		}),
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/request", strings.NewReader(`{"email":"ada@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res := httptest.NewRecorder()
	sessions.Middleware(magic.RequestHandler()).ServeHTTP(res, req)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", res.Code)
	}
}

// TestWebAuthnRequiresConfiguredOrigin proves every ceremony fails closed while
// WebAuthnOptions.Origin is empty. Before the fix the check fell back to the
// request headers, so a forged X-Forwarded-Host satisfied it.
func TestWebAuthnRequiresConfiguredOrigin(t *testing.T) {
	sessions := session.MustNew("webauthn-origin-secret", session.Options{})
	authn := New(sessions, Options{})
	webauthn := authn.WebAuthn(WebAuthnOptions{RPName: "GoSX"})

	var beginErr error
	handler := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, beginErr = webauthn.BeginRegistration(r, User{ID: "ada@example.com"}, "")
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/begin", nil)
	req.Header.Set("X-Forwarded-Host", "attacker.example")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if !errors.Is(beginErr, ErrOriginNotConfigured) {
		t.Fatalf("BeginRegistration error = %v, want ErrOriginNotConfigured", beginErr)
	}

	var loginErr error
	loginHandler := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, loginErr = webauthn.BeginAuthentication(r, "ada@example.com", "")
		w.WriteHeader(http.StatusNoContent)
	}))
	loginHandler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/begin", nil))
	if !errors.Is(loginErr, ErrOriginNotConfigured) {
		t.Fatalf("BeginAuthentication error = %v, want ErrOriginNotConfigured", loginErr)
	}
}

// TestWebAuthnForgedForwardedHostFailsOriginCheck drives a registration that is
// valid in every part except the origin. The client data names the attacker
// origin, and the request carries the matching forged headers. The ceremony
// must fail, whether or not the application configures an origin.
func TestWebAuthnForgedForwardedHostFailsOriginCheck(t *testing.T) {
	cases := []struct {
		name   string
		origin string
	}{
		{name: "origin configured", origin: "https://app.example"},
		{name: "origin missing", origin: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			sessions := session.MustNew("webauthn-forged-secret", session.Options{})
			authn := New(sessions, Options{})
			store := NewMemoryWebAuthnStore()
			webauthn := authn.WebAuthn(WebAuthnOptions{
				RPID:   "attacker.example",
				Origin: testCase.origin,
				Store:  store,
			})

			privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
			if err != nil {
				t.Fatal(err)
			}
			rawID := mustRandomBytes(t, 32)

			// Seed the ceremony state directly. The missing-origin case must
			// then reach the origin comparison in FinishRegistration.
			state := webAuthnState{
				Kind:      webAuthnStateRegister,
				Challenge: "test-challenge",
				User:      User{ID: "ada@example.com", Email: "ada@example.com"},
				ExpiresAt: webauthn.now().Add(webauthn.ttl),
			}
			var finishErr error
			handler := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				session.Current(r).Set(webauthn.sessionKey, state)
				payload := WebAuthnRegistrationResponse{
					ID:    encodeWebAuthnBytes(rawID),
					RawID: encodeWebAuthnBytes(rawID),
					Type:  "public-key",
				}
				payload.Response.ClientDataJSON = encodeWebAuthnBytes(mustJSONBytes(t, webAuthnClientData{
					Type:      "webauthn.create",
					Challenge: state.Challenge,
					Origin:    "https://attacker.example",
				}))
				payload.Response.AuthenticatorData = encodeWebAuthnBytes(webAuthnAuthData("attacker.example", 0x45, 0))
				payload.Response.PublicKey = encodeWebAuthnBytes(publicKeyDER)
				payload.Response.PublicKeyAlgorithm = -7
				_, _, finishErr = webauthn.FinishRegistration(r, payload)
				w.WriteHeader(http.StatusNoContent)
			}))

			req := httptest.NewRequest(http.MethodPost, "/finish", nil)
			req.Host = "attacker.example"
			req.Header.Set("X-Forwarded-Host", "attacker.example")
			req.Header.Set("X-Forwarded-Proto", "https")
			handler.ServeHTTP(httptest.NewRecorder(), req)

			if finishErr == nil {
				t.Fatal("forged forwarded host satisfied the webauthn origin check")
			}
			if _, err := store.Credential(encodeWebAuthnBytes(rawID)); err == nil {
				t.Fatal("forged ceremony stored a credential")
			}
		})
	}
}

// TestSanitizeRedirectTarget covers the open-redirect shapes that browsers
// normalize, including the backslash form that passed before the fix.
func TestSanitizeRedirectTarget(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty", value: "", want: ""},
		{name: "plain path", value: "/admin", want: "/admin"},
		{name: "path with query", value: "/admin?tab=1", want: "/admin?tab=1"},
		{name: "backslash host", value: `/\evil.example`, want: ""},
		{name: "backslash slash host", value: `/\/evil.example`, want: ""},
		{name: "double slash host", value: "//evil.example", want: ""},
		{name: "absolute https", value: "https://evil.example", want: ""},
		{name: "absolute http", value: "http://evil.example", want: ""},
		{name: "encoded backslash", value: "/%5cevil.example", want: ""},
		{name: "encoded backslash upper", value: "/%5Cevil.example", want: ""},
		{name: "tab inside", value: "/\tevil.example", want: ""},
		{name: "newline inside", value: "/admin\n/evil", want: ""},
		{name: "carriage return", value: "/admin\r\nLocation:/evil", want: ""},
		{name: "relative path", value: "admin", want: ""},
		{name: "scheme relative with backslash", value: `\\evil.example`, want: ""},
		{name: "javascript scheme", value: "javascript:alert(1)", want: ""},
		{name: "userinfo", value: "/@evil.example", want: "/@evil.example"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := sanitizeRedirectTarget(testCase.value); got != testCase.want {
				t.Fatalf("sanitizeRedirectTarget(%q) = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

// TestRedirectBackTargetDropsCrossSiteReferer proves a cross-site Referer
// cannot steer the redirect that follows a magic-link request.
func TestRedirectBackTargetDropsCrossSiteReferer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/request", nil)
	req.Host = "app.example"
	req.Header.Set("Referer", "https://attacker.example/steal")
	if got := redirectBackTarget(req, "/login"); got != "/auth/magic-link/request" {
		t.Fatalf("redirectBackTarget = %q, want the request path", got)
	}

	same := httptest.NewRequest(http.MethodPost, "/auth/magic-link/request", nil)
	same.Host = "app.example"
	same.Header.Set("Referer", "https://app.example/docs/auth?tab=1")
	if got := redirectBackTarget(same, "/login"); got != "/docs/auth?tab=1" {
		t.Fatalf("redirectBackTarget = %q, want the same-site path", got)
	}
}
