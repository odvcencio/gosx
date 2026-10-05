package auth

import (
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/session"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestJSONSignInReturnsRotatedTokenForNextMutation(t *testing.T) {
	sessions := session.MustNew("json-signin-rotation-secret", session.Options{})
	authn := New(sessions, Options{})
	w := httptest.NewRecorder()
	var previous string
	sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessions.Get(r).Set("pending", true)
		previous = session.Token(r)
	})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	cookie := w.Result().Cookies()[0]
	r := httptest.NewRequest("POST", "/login", strings.NewReader(`{}`))
	r.AddCookie(cookie)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", previous)
	w = httptest.NewRecorder()
	sessions.Middleware(sessions.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action.ServeHandler(w, r, func(ctx *action.Context) error {
			if !authn.SignIn(ctx.Request, User{ID: "user"}) {
				t.Fatal("sign in failed")
			}
			return ctx.Success("signed in", nil)
		})
	}))).ServeHTTP(w, r)
	fresh := w.Header().Get("X-CSRF-Token")
	if w.Code != 200 || fresh == "" || fresh == previous {
		t.Fatalf("sign-in status=%d token=%q", w.Code, fresh)
	}
	cookie = w.Result().Cookies()[0]
	for _, tc := range []struct {
		token string
		want  int
	}{{previous, 403}, {fresh, 204}} {
		r := httptest.NewRequest("POST", "/save", nil)
		r.AddCookie(cookie)
		r.Header.Set("X-CSRF-Token", tc.token)
		w := httptest.NewRecorder()
		sessions.Middleware(sessions.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if user, ok := authn.Current(r); !ok || user.ID != "user" {
				t.Fatal("signed-in user lost")
			}
			w.WriteHeader(204)
		}))).ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("next mutation status=%d, want %d", w.Code, tc.want)
		}
	}
}

func TestSignOutPersistsNewNoticeAndNativeActionResult(t *testing.T) {
	sessions := session.MustNew("signout-feedback-secret", session.Options{})
	authn := New(sessions, Options{})
	w := httptest.NewRecorder()
	var token string
	sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authn.SignIn(r, User{ID: "user"})
		sessions.Get(r).Set("private", "old")
		sessions.Get(r).AddFlash("old", "discard")
		token = session.Token(r)
	})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	r := httptest.NewRequest("POST", "/logout", strings.NewReader(url.Values{"csrf_token": {token}}.Encode()))
	r.AddCookie(w.Result().Cookies()[0])
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Referer", "/account")
	w = httptest.NewRecorder()
	sessions.Middleware(sessions.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action.ServeHandler(w, r, func(ctx *action.Context) error {
			authn.SignOut(ctx.Request)
			session.AddFlash(ctx.Request, "notice", "Signed out")
			return ctx.Success("Signed out", nil)
		})
	}))).ServeHTTP(w, r)
	if w.Code != 303 {
		t.Fatalf("logout status=%d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge < 0 {
		t.Fatal("new anonymous feedback session missing")
	}
	r = httptest.NewRequest("GET", "/account", nil)
	r.AddCookie(cookies[0])
	sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authn.Current(r); ok {
			t.Fatal("sign-out retained user")
		}
		store := sessions.Get(r)
		if store.Value("private") != nil || len(store.Flashes("old")) != 0 {
			t.Fatal("old session state survived sign-out")
		}
		if got := store.Flashes("notice"); len(got) != 1 || got[0] != "Signed out" {
			t.Fatalf("notice=%v", got)
		}
		states := action.States(r)
		if len(states) != 1 {
			t.Fatalf("action states=%v", states)
		}
		for _, view := range states {
			if !view.OK() || view.Message() != "Signed out" {
				t.Fatalf("action result=%v", view)
			}
		}
		if fresh := session.Token(r); fresh == "" || fresh == token {
			t.Fatal("anonymous session token not rotated")
		}
	})).ServeHTTP(httptest.NewRecorder(), r)
}
