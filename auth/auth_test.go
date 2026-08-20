package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx/session"
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

func TestRequirePreservesCanonicalTargetOnlyForGetAndHead(t *testing.T) {
	sessions := session.MustNew("auth-return-path-secret", session.Options{})
	authn := New(sessions, Options{LoginPath: "/login?source=protected"})
	protected := sessions.Middleware(authn.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("anonymous request reached protected handler")
	})))

	tests := []struct {
		name       string
		method     string
		requestURI string
		want       string
	}{
		{name: "get canonicalizes", method: http.MethodGet, requestURI: "/draft//room/../admin?tab=caf%C3%A9", want: "/login?source=protected&next=%2Fdraft%2Fadmin%3Ftab%3Dcaf%25C3%25A9"},
		{name: "head preserves", method: http.MethodHead, requestURI: "/settings?tab=security", want: "/login?source=protected&next=%2Fsettings%3Ftab%3Dsecurity"},
		{name: "post does not replay", method: http.MethodPost, requestURI: "/settings?tab=security", want: "/login?source=protected"},
		{name: "put does not replay", method: http.MethodPut, requestURI: "/settings", want: "/login?source=protected"},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			res := httptest.NewRecorder()
			protected.ServeHTTP(res, httptest.NewRequest(testCase.method, testCase.requestURI, nil))
			if res.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", res.Code)
			}
			if got := res.Header().Get("Location"); got != testCase.want {
				t.Fatalf("Location = %q, want canonical exact target %q", got, testCase.want)
			}
		})
	}
}

func TestRequireFallsBackToRootForUnsafeGetTarget(t *testing.T) {
	sessions := session.MustNew("auth-return-path-invalid-secret", session.Options{})
	authn := New(sessions, Options{LoginPath: "/login"})
	protected := sessions.Middleware(authn.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("anonymous request reached protected handler")
	})))

	for _, requestURI := range []string{"//evil.example/steal", "/draft%0aLocation:%20/evil", "/draft/../admin"} {
		res := httptest.NewRecorder()
		protected.ServeHTTP(res, httptest.NewRequest(http.MethodGet, requestURI, nil))
		if res.Code != http.StatusSeeOther {
			t.Fatalf("%q status = %d, want 303", requestURI, res.Code)
		}
		if got := res.Header().Get("Location"); requestURI == "/draft/../admin" {
			want := "/login?next=%2Fadmin"
			if got != want {
				t.Fatalf("%q Location = %q, want %q", requestURI, got, want)
			}
		} else if got != "/login?next=%2F" {
			t.Fatalf("%q Location = %q, want root fallback", requestURI, got)
		}
	}
}

func TestManagerCanonicalizesConfiguredLoginPath(t *testing.T) {
	sessions := session.MustNew("auth-login-path-secret", session.Options{})
	authn := New(sessions, Options{LoginPath: "https://evil.example/login"})
	protected := sessions.Middleware(authn.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("anonymous request reached protected handler")
	})))
	res := httptest.NewRecorder()
	protected.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if got := res.Header().Get("Location"); got != "/login?next=%2Fsettings" {
		t.Fatalf("Location = %q, want safe configured-path fallback", got)
	}
}

func TestManagerSupportsCustomProvider(t *testing.T) {
	authn := New(nil, Options{
		Provider: ProviderFunc(func(r *http.Request) (User, bool) {
			return User{ID: "provider-user", Name: "Lin"}, true
		}),
	})

	handler := authn.Middleware(authn.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := Current(r)
		if !ok || user.Name != "Lin" {
			t.Fatalf("expected provider-backed current user, got %#v ok=%v", user, ok)
		}
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
