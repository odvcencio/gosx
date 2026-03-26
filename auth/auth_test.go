package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/odvcencio/gosx/session"
)

func TestRequireRedirectsAndLoadsCurrentUser(t *testing.T) {
	sessions := session.MustNew("auth-test-secret-value", session.Options{})
	authn := New(sessions, Options{LoginPath: "/login"})

	signIn := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authn.SignIn(r, User{ID: "u_123", Name: "Ada", Roles: []string{"admin"}}) {
			t.Fatal("expected sign-in to succeed")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	protected := sessions.Middleware(authn.Middleware(authn.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := Current(r)
		if !ok || user.Name != "Ada" {
			t.Fatalf("expected current user in context, got %#v ok=%v", user, ok)
		}
		w.WriteHeader(http.StatusOK)
	}))))

	anonReq := httptest.NewRequest(http.MethodGet, "/settings", nil)
	anonRes := httptest.NewRecorder()
	protected.ServeHTTP(anonRes, anonReq)
	if anonRes.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", anonRes.Code)
	}
	if location := anonRes.Header().Get("Location"); !strings.HasPrefix(location, "/login?next=%2Fsettings") {
		t.Fatalf("unexpected redirect location %q", location)
	}

	signInReq := httptest.NewRequest(http.MethodPost, "/login", nil)
	signInRes := httptest.NewRecorder()
	signIn.ServeHTTP(signInRes, signInReq)
	if signInRes.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", signInRes.Code)
	}
	cookie := signInRes.Result().Cookies()[0]

	authReq := httptest.NewRequest(http.MethodGet, "/settings", nil)
	authReq.AddCookie(cookie)
	authRes := httptest.NewRecorder()
	protected.ServeHTTP(authRes, authReq)
	if authRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", authRes.Code)
	}
}
