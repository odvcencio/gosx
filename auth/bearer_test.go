package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequireBearerToken(t *testing.T) {
	const token = "secret-token_123+/=="

	tests := []struct {
		name                 string
		configuredToken      string
		opts                 BearerOptions
		authorization        []string
		wantStatus           int
		wantChallenge        string
		wantNextCalls        int
		wantResponseContains string
	}{
		{
			name:            "accepts case insensitive scheme and optional whitespace",
			configuredToken: token,
			authorization:   []string{"\t bEaReR\t" + token + "  "},
			wantStatus:      http.StatusNoContent,
			wantNextCalls:   1,
		},
		{
			name:                 "missing credential",
			configuredToken:      token,
			wantStatus:           http.StatusUnauthorized,
			wantChallenge:        `Bearer realm="restricted"`,
			wantResponseContains: "Unauthorized",
		},
		{
			name:                 "wrong credential",
			configuredToken:      token,
			authorization:        []string{"Bearer wrong-token"},
			wantStatus:           http.StatusUnauthorized,
			wantChallenge:        `Bearer realm="restricted"`,
			wantResponseContains: "Unauthorized",
		},
		{
			name:            "wrong scheme",
			configuredToken: token,
			authorization:   []string{"Basic " + token},
			wantStatus:      http.StatusUnauthorized,
			wantChallenge:   `Bearer realm="restricted"`,
		},
		{
			name:            "missing credential after scheme",
			configuredToken: token,
			authorization:   []string{"Bearer"},
			wantStatus:      http.StatusUnauthorized,
			wantChallenge:   `Bearer realm="restricted"`,
		},
		{
			name:            "extra credential",
			configuredToken: token,
			authorization:   []string{"Bearer " + token + " extra"},
			wantStatus:      http.StatusUnauthorized,
			wantChallenge:   `Bearer realm="restricted"`,
		},
		{
			name:            "comma separated credentials",
			configuredToken: token,
			authorization:   []string{"Bearer " + token + ", Bearer " + token},
			wantStatus:      http.StatusUnauthorized,
			wantChallenge:   `Bearer realm="restricted"`,
		},
		{
			name:            "duplicate authorization headers",
			configuredToken: token,
			authorization:   []string{"Bearer " + token, "Bearer " + token},
			wantStatus:      http.StatusUnauthorized,
			wantChallenge:   `Bearer realm="restricted"`,
		},
		{
			name:            "invalid token character",
			configuredToken: token,
			authorization:   []string{"Bearer secret token"},
			wantStatus:      http.StatusUnauthorized,
			wantChallenge:   `Bearer realm="restricted"`,
		},
		{
			name:            "unconfigured is unauthorized by default",
			configuredToken: "",
			authorization:   []string{"Bearer " + token},
			wantStatus:      http.StatusUnauthorized,
			wantChallenge:   `Bearer realm="restricted"`,
		},
		{
			name:            "unconfigured can be hidden",
			configuredToken: "",
			opts:            BearerOptions{HideWhenUnconfigured: true},
			authorization:   []string{"Bearer " + token},
			wantStatus:      http.StatusNotFound,
			wantNextCalls:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nextCalls := 0
			handler := RequireBearerToken(tc.configuredToken, tc.opts)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalls++
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "/private", nil)
			for _, value := range tc.authorization {
				req.Header.Add("Authorization", value)
			}
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)

			if res.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", res.Code, tc.wantStatus, res.Body.String())
			}
			if nextCalls != tc.wantNextCalls {
				t.Fatalf("next calls = %d, want %d", nextCalls, tc.wantNextCalls)
			}
			if got := res.Header().Get("WWW-Authenticate"); got != tc.wantChallenge {
				t.Fatalf("WWW-Authenticate = %q, want %q", got, tc.wantChallenge)
			}
			if tc.wantResponseContains != "" && !strings.Contains(res.Body.String(), tc.wantResponseContains) {
				t.Fatalf("body = %q, want it to contain %q", res.Body.String(), tc.wantResponseContains)
			}
		})
	}
}

func TestRequireBearerTokenEscapesRealm(t *testing.T) {
	handler := RequireBearerToken("secret-token", BearerOptions{Realm: `league "alpha"\east`})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler should not run")
	}))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/private", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.Code)
	}
	want := `Bearer realm="league \"alpha\"\\east"`
	if got := res.Header().Get("WWW-Authenticate"); got != want {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestRequireBearerTokenEscapesRealmControls(t *testing.T) {
	handler := RequireBearerToken("secret-token", BearerOptions{Realm: "private\r\nInjected: yes"})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler should not run")
	}))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/private", nil))
	want := `Bearer realm="private\x0D\x0AInjected: yes"`
	if got := res.Header().Get("WWW-Authenticate"); got != want {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
	}
	if strings.ContainsAny(res.Header().Get("WWW-Authenticate"), "\r\n") {
		t.Fatal("WWW-Authenticate contained a raw control character")
	}
}

func TestRequireBearerTokenDoesNotLeakCredential(t *testing.T) {
	const token = "do-not-leak-this-secret"
	handler := RequireBearerToken(token, BearerOptions{Realm: "private"})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler should not run")
	}))
	for _, tc := range []struct {
		name          string
		authorization string
	}{
		{name: "wrong token", authorization: "Bearer wrong"},
		{name: "extra credential", authorization: "Bearer " + token + " extra"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/private", nil)
			req.Header.Set("Authorization", tc.authorization)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if strings.Contains(res.Body.String(), token) || strings.Contains(res.Header().Get("WWW-Authenticate"), token) {
				t.Fatal("response leaked the configured credential")
			}
		})
	}
}

func TestRequireBearerTokenNilNext(t *testing.T) {
	handler := RequireBearerToken("secret-token", BearerOptions{})(nil)
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
}

func TestValidBearerToken(t *testing.T) {
	valid := []string{"a", "abc123", "abc-._~+/", "abc=="}
	for _, token := range valid {
		t.Run(fmt.Sprintf("valid_%q", token), func(t *testing.T) {
			if !validBearerToken(token) {
				t.Fatalf("validBearerToken(%q) = false", token)
			}
		})
	}
	invalid := []string{"", "=abc", "abc=def", "abc def", "abc,def", "abc\rdef", "abc\u2003def"}
	for _, token := range invalid {
		t.Run(fmt.Sprintf("invalid_%q", token), func(t *testing.T) {
			if validBearerToken(token) {
				t.Fatalf("validBearerToken(%q) = true", token)
			}
		})
	}
}

func FuzzParseBearerAuthorization(f *testing.F) {
	for _, seed := range []string{"Bearer token", "bEaReR abc==", "Basic token", "Bearer", "Bearer token extra"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		token, ok := parseBearerAuthorization(value)
		if ok {
			if !validBearerToken(token) {
				t.Fatalf("accepted invalid credential %q", token)
			}
			if strings.IndexAny(token, " \t") >= 0 {
				t.Fatalf("accepted credential with whitespace %q", token)
			}
		}
	})
}
