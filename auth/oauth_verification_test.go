package auth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type oauthVerificationTransport func(*http.Request) (*http.Response, error)

func (fn oauthVerificationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestOAuthEmailRequiresBooleanVerification(t *testing.T) {
	for _, claim := range []any{nil, false, "true", 1, true} {
		payload := map[string]any{"sub": "subject", "email": "claimed@example.invalid", "email_verified": claim}
		user, err := normalizeOAuthUser("oidc", userFromOAuthPayload("oidc", payload), nil)
		if err != nil {
			t.Fatal(err)
		}
		verified := claim == true
		if user.EmailVerified != verified || (user.Email != "") != verified || user.ID != "oidc:subject" {
			t.Fatalf("claim %v: %#v", claim, user)
		}
	}
	user, err := normalizeOAuthUser("custom", User{ID: "subject", Email: "claimed@example.invalid"}, nil)
	if err != nil || user.Email != "" || user.EmailVerified {
		t.Fatalf("unverified custom resolver: %#v %v", user, err)
	}
	if _, err := normalizeOAuthUser("oidc", userFromOAuthPayload("oidc", map[string]any{"email": "claimed@example.invalid"}), nil); err == nil {
		t.Fatal("unverified email became an identity")
	}
}

func TestGitHubOAuthUsesOnlyVerifiedAddresses(t *testing.T) {
	for _, tc := range []struct{ name, emails, want string }{
		{"unverified primary", `[{"email":"claimed@example.invalid","primary":true,"verified":false}]`, ""},
		{"unverified fallback", `[{"email":"claimed@example.invalid","verified":false}]`, ""},
		{"verified primary", `[{"email":"verified@example.invalid","primary":true,"verified":true}]`, "verified@example.invalid"},
		{"verified fallback", `[{"email":"claimed@example.invalid","primary":true,"verified":false},{"email":"verified@example.invalid","verified":true}]`, "verified@example.invalid"},
		{"string claim", `[{"email":"claimed@example.invalid","primary":true,"verified":"true"}]`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: oauthVerificationTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				body := `{"id":123,"email":"claimed@example.invalid"}`
				if r.URL.Path == "/user/emails" {
					body = tc.emails
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			resolver := githubOAuthResolver()
			user, err := resolver.ResolveOAuthUser(context.Background(), OAuthProvider{Name: "github", UserInfoURL: "https://provider.example/user"}, client, OAuthToken{AccessToken: "token"})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 || user.Email != tc.want || user.EmailVerified != (tc.want != "") || user.ID != "github:123" {
				t.Fatalf("user=%#v calls=%d", user, calls)
			}
		})
	}
}
