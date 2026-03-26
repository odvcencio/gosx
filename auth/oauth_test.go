package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/odvcencio/gosx/session"
)

func TestOAuthCallbackSignsIn(t *testing.T) {
	var exchangedVerifier string
	var exchangedCode string
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			exchangedVerifier = r.Form.Get("code_verifier")
			exchangedCode = r.Form.Get("code")
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"access_token":"token_123","token_type":"Bearer"}`))
		case "/userinfo":
			if got := r.Header.Get("Authorization"); got != "Bearer token_123" {
				t.Fatalf("unexpected auth header %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"sub":"user-123","email":"ada@example.com","name":"Ada"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer providerServer.Close()

	sessions := session.MustNew("oauth-test-secret", session.Options{})
	authn := New(sessions, Options{LoginPath: "/login"})
	oauth := authn.OAuth(OAuthOptions{
		HTTPClient: providerServer.Client(),
		Providers: []OAuthProvider{
			{
				Name:         "demo",
				ClientID:     "client-id",
				ClientSecret: "client-secret",
				AuthorizeURL: providerServer.URL + "/authorize",
				TokenURL:     providerServer.URL + "/token",
				RedirectURL:  "http://localhost/auth/oauth/demo/callback",
				UserInfoURL:  providerServer.URL + "/userinfo",
				Scopes:       []string{"openid", "email", "profile"},
			},
		},
	})

	beginHandler := sessions.Middleware(oauth.BeginHandler("demo"))
	callbackHandler := sessions.Middleware(authn.Middleware(oauth.CallbackHandler("demo")))
	protected := sessions.Middleware(authn.Middleware(authn.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := Current(r)
		if !ok || user.Email != "ada@example.com" {
			t.Fatalf("expected oauth user in session, got %#v ok=%v", user, ok)
		}
		w.WriteHeader(http.StatusOK)
	}))))

	beginReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/demo?next=/admin", nil)
	beginRes := httptest.NewRecorder()
	beginHandler.ServeHTTP(beginRes, beginReq)
	if beginRes.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected 307, got %d", beginRes.Code)
	}
	location := beginRes.Header().Get("Location")
	if !strings.HasPrefix(location, providerServer.URL+"/authorize?") {
		t.Fatalf("unexpected authorize location %q", location)
	}
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatal("expected oauth state in authorize url")
	}
	if parsed.Query().Get("code_challenge") == "" {
		t.Fatal("expected pkce challenge in authorize url")
	}
	beginCookie := firstCookie(beginRes)
	if beginCookie == nil {
		t.Fatal("expected oauth session cookie")
	}

	callbackReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/demo/callback?code=oauth-code&state="+url.QueryEscape(state), nil)
	callbackReq.AddCookie(beginCookie)
	callbackRes := httptest.NewRecorder()
	callbackHandler.ServeHTTP(callbackRes, callbackReq)
	if callbackRes.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", callbackRes.Code, callbackRes.Body.String())
	}
	if location := callbackRes.Header().Get("Location"); location != "/admin" {
		t.Fatalf("expected redirect to /admin, got %q", location)
	}
	if exchangedCode != "oauth-code" {
		t.Fatalf("expected code exchange, got %q", exchangedCode)
	}
	if exchangedVerifier == "" {
		t.Fatal("expected code verifier during token exchange")
	}

	authCookie := firstCookie(callbackRes)
	if authCookie == nil {
		t.Fatal("expected auth cookie")
	}
	protectedReq := httptest.NewRequest(http.MethodGet, "/protected", nil)
	protectedReq.AddCookie(authCookie)
	protectedRes := httptest.NewRecorder()
	protected.ServeHTTP(protectedRes, protectedReq)
	if protectedRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", protectedRes.Code)
	}
}

func TestOAuthCallbackRejectsInvalidState(t *testing.T) {
	sessions := session.MustNew("oauth-state-secret", session.Options{})
	authn := New(sessions, Options{})
	oauth := authn.OAuth(OAuthOptions{
		Providers: []OAuthProvider{
			{
				Name:         "demo",
				ClientID:     "client-id",
				ClientSecret: "client-secret",
				AuthorizeURL: "https://provider.example/authorize",
				TokenURL:     "https://provider.example/token",
				RedirectURL:  "http://localhost/auth/oauth/demo/callback",
				UserInfoURL:  "https://provider.example/userinfo",
			},
		},
	})

	var target string
	begin := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		target, err = oauth.Begin(r, "demo", "/")
		if err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	beginRes := httptest.NewRecorder()
	begin.ServeHTTP(beginRes, httptest.NewRequest(http.MethodGet, "/begin", nil))
	if target == "" {
		t.Fatal("expected oauth begin target")
	}
	beginCookie := firstCookie(beginRes)
	if beginCookie == nil {
		t.Fatal("expected oauth begin cookie")
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/demo/callback?code=x&state=wrong", nil)
	req.AddCookie(beginCookie)
	res := httptest.NewRecorder()
	sessions.Middleware(oauth.CallbackHandler("demo")).ServeHTTP(res, req)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 failure redirect, got %d", res.Code)
	}
}
