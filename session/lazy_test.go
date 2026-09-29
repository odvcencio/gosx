package session

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnonymousTokenReadDoesNotCreateSession(t *testing.T) {
	m := MustNew("lazy-session-test-secret", Options{})
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Token(r) != "" || m.Token(r) != "" || HasState(r) {
			t.Fatal("anonymous token read created session state")
		}
		w.Header().Set("Vary", "Accept-Encoding")
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "https://app.example/", nil))
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("anonymous response set a cookie")
	}
	if got := strings.Join(w.Header().Values("Vary"), ","); got != "Accept-Encoding,Cookie" {
		t.Fatalf("Vary = %q", got)
	}
}

func TestSessionWritesAndReadsStayPrivate(t *testing.T) {
	for _, operation := range []string{"value", "flash", "destroy"} {
		t.Run(operation, func(t *testing.T) {
			m := MustNew("lazy-session-test-secret", Options{})
			h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/write" {
					switch operation {
					case "value":
						Current(r).Set("name", "Ada")
					case "flash":
						AddFlash(r, "notice", "saved")
					case "destroy":
						Destroy(r)
					}
				} else if !HasState(r) || Token(r) == "" {
					t.Fatal("persisted session has no token")
				}
				w.Header().Set("Cache-Control", "public, max-age=60")
				w.WriteHeader(http.StatusOK)
			}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/write", nil))
			if len(w.Result().Cookies()) != 1 || w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("session write headers = %v", w.Header())
			}
			if operation != "destroy" {
				r := httptest.NewRequest(http.MethodGet, "/read", nil)
				r.AddCookie(w.Result().Cookies()[0])
				w = httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Header().Get("Cache-Control") != "private, no-store" {
					t.Fatalf("session read headers = %v", w.Header())
				}
			}
		})
	}
}

func TestProtectOriginAndSessionToken(t *testing.T) {
	m := MustNew("origin-session-test-secret", Options{})
	cookie := issueCookie(t, m, "name", "Ada")
	envelope, err := m.decode(cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	token := envelope.Values[defaultCSRFKey].(string)
	h := m.Middleware(m.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	cases := []struct {
		name, site, origin string
		hasToken           bool
		anonymous, session int
	}{
		{"same origin metadata", "same-origin", "", false, 204, 403},
		{"same origin header", "", "https://app.example", false, 204, 403},
		{"same origin token", "same-origin", "https://app.example", true, 204, 204},
		{"cross site", "cross-site", "", true, 403, 403},
		{"same site", "same-site", "https://app.example", true, 403, 403},
		{"foreign origin", "", "https://foreign.example", true, 403, 403},
		{"contradictory origin", "same-origin", "https://foreign.example", true, 403, 403},
		{"different scheme", "", "http://app.example", true, 403, 403},
		{"null origin", "", "null", true, 403, 403},
		{"no metadata or token", "", "", false, 403, 403},
		{"legacy token", "", "", true, 403, 204},
		{"none is not proof", "none", "", false, 403, 403},
	}
	for _, tc := range cases {
		for _, withSession := range []bool{false, true} {
			name := tc.name + "/anonymous"
			want := tc.anonymous
			if withSession {
				name, want = tc.name+"/session", tc.session
			}
			t.Run(name, func(t *testing.T) {
				r := httptest.NewRequest(http.MethodPost, "https://app.example/form", nil)
				r.Header.Set("Sec-Fetch-Site", tc.site)
				r.Header.Set("Origin", tc.origin)
				if tc.hasToken {
					r.Header.Set("X-CSRF-Token", token)
				}
				if withSession {
					r.AddCookie(cookie)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != want {
					t.Fatalf("status = %d, want %d", w.Code, want)
				}
				if !withSession && len(w.Result().Cookies()) != 0 {
					t.Fatal("CSRF guard minted an anonymous cookie")
				}
			})
		}
	}
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete, "CUSTOM"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "https://app.example/form", nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("unprotected unsafe method %s: %d", method, w.Code)
		}
	}
}

func TestProtectTrustedOriginsAndForwardedAllowlist(t *testing.T) {
	m, err := New("origin-session-test-secret", Options{
		TrustedOrigins: []string{"https://trusted.example"},
		AllowedHosts:   []string{"app.example", "internal.example"},
		ForwardedTrust: ForwardedTrust{Enabled: true, Proxies: []string{"192.0.2.0/24"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := m.Middleware(m.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "accepted")
	})))
	for _, tc := range []struct {
		name, peer, host, origin, site string
		want                           int
	}{
		{"trusted proxy", "192.0.2.1:80", "app.example", "https://app.example", "", 200},
		{"untrusted proxy", "198.51.100.1:80", "app.example", "https://app.example", "", 403},
		{"forwarded host outside allowlist", "192.0.2.1:80", "foreign.example", "https://foreign.example", "", 403},
		{"explicit trusted origin", "198.51.100.1:80", "", "https://trusted.example", "cross-site", 200},
		{"trusted proxy foreign origin", "192.0.2.1:80", "app.example", "https://foreign.example", "same-origin", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://internal.example/form", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			r.Header.Set("X-Forwarded-Host", tc.host)
			r.Header.Set("X-Forwarded-Proto", "https")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}

func TestOriginOptionsRejectInvalidTrust(t *testing.T) {
	for _, opts := range []Options{
		{TrustedOrigins: []string{"https://app.example/path"}},
		{TrustedOrigins: []string{"https://user@app.example"}},
		{AllowedHosts: []string{"app.example/path"}},
		{ForwardedTrust: ForwardedTrust{Enabled: true}},
		{AllowedHosts: []string{"app.example"}, ForwardedTrust: ForwardedTrust{Enabled: true, Proxies: []string{"bad"}}},
	} {
		if _, err := New("origin-session-test-secret", opts); err == nil {
			t.Fatalf("accepted invalid origin options: %+v", opts)
		}
	}
}
