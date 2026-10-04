package auth

import (
	"m31labs.dev/gosx/session"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSignInRenewsSessionAndSignOutDestroysIt(t *testing.T) {
	sessions := session.MustNew("auth-rotation-test-secret", session.Options{})
	authn := New(sessions, Options{})
	var before, after string
	w := httptest.NewRecorder()
	sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store := sessions.Get(r)
		store.Set("pending-auth-flow", "state")
		store.AddFlash("old", "state")
		session.Token(r)
		before = store.String("__gosx_csrf")
		if !authn.SignIn(r, User{ID: "test-user"}) {
			t.Fatal("sign in failed")
		}
		session.Token(r)
		after = store.String("__gosx_csrf")
		if before == after || store.Value("pending-auth-flow") != nil || len(store.Flashes("old")) != 0 {
			t.Fatal("sign in retained old session state")
		}
	})).ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	cookie := w.Result().Cookies()[0]
	for _, token := range []string{before, after} {
		request := httptest.NewRequest("POST", "/", nil)
		request.AddCookie(cookie)
		request.Header.Set("X-CSRF-Token", token)
		response := httptest.NewRecorder()
		sessions.Middleware(sessions.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))).ServeHTTP(response, request)
		want := 204
		if token == before {
			want = 403
		}
		if response.Code != want {
			t.Fatalf("token after sign in: status=%d want=%d", response.Code, want)
		}
	}

	r := httptest.NewRequest("POST", "/", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authn.SignOut(r)
		if len(sessions.Get(r).Values()) != 0 || session.Token(r) != "" {
			t.Fatal("sign out retained session state")
		}
	})).ServeHTTP(w, r)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatal("sign out did not expire cookie")
	}
}
