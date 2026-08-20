package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/session"
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

	beginReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/demo?next="+url.QueryEscape("/draft//room/../admin?tab=café"), nil)
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
	if location := callbackRes.Header().Get("Location"); location != "/draft/admin?tab=caf%C3%A9" {
		t.Fatalf("expected exact canonical redirect, got %q", location)
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
		SuccessPath: "/success//flow/../done",
		FailurePath: "/failure//oauth/../retry",
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
	if location := res.Header().Get("Location"); location != "/failure/retry" {
		t.Fatalf("failure Location = %q, want canonical configured path", location)
	}
}

func TestOAuthCanonicalizesConfiguredAndRequestedTargets(t *testing.T) {
	sessions := session.MustNew("oauth-return-path-secret", session.Options{})
	authn := New(sessions, Options{})
	oauth := authn.OAuth(OAuthOptions{
		SuccessPath: "/success//flow/../done",
		FailurePath: "https://evil.example/failure",
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
	if oauth.successPath != "/success/done" {
		t.Fatalf("successPath = %q, want canonical configured path", oauth.successPath)
	}
	if oauth.failurePath != "/" {
		t.Fatalf("failurePath = %q, want root fallback", oauth.failurePath)
	}

	tests := []struct {
		name string
		next string
		want string
	}{
		{name: "canonicalizes", next: "/draft//room/../admin?tab=café", want: "/draft/admin?tab=caf%C3%A9"},
		{name: "external falls back", next: "https://evil.example/steal", want: "/"},
		{name: "protocol relative falls back", next: "//evil.example/steal", want: "/"},
		{name: "control falls back", next: "/draft%0aLocation:%20/evil", want: "/"},
		{name: "omitted remains omitted", next: "", want: ""},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			var states map[string]oauthState
			handler := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := oauth.Begin(r, "demo", testCase.next); err != nil {
					t.Fatal(err)
				}
				if !session.Current(r).Decode(oauth.sessionKey, &states) {
					t.Fatal("oauth state was not persisted")
				}
				if len(states) != 1 {
					t.Fatalf("state map length = %d, want 1", len(states))
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/begin", nil))
			for _, state := range states {
				if state.Next != testCase.want {
					t.Fatalf("state.Next = %q, want canonical %q", state.Next, testCase.want)
				}
			}
		})
	}
}
func TestOAuthSequentialBrowserTabCallbacksPreserveEitherOrder(t *testing.T) {
	for _, order := range [][]int{{0, 1}, {1, 0}} {
		order := order
		t.Run("callback-order-"+string(rune('0'+order[0]))+string(rune('0'+order[1])), func(t *testing.T) {
			providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/token" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"token_123"}`))
			}))
			defer providerServer.Close()

			sessions := session.MustNew("oauth-tabs-secret-value", session.Options{})
			authn := New(sessions, Options{})
			oauth := authn.OAuth(OAuthOptions{
				HTTPClient: providerServer.Client(),
				Providers:  []OAuthProvider{testOAuthProvider(providerServer.URL)},
			})
			beginHandler := sessions.Middleware(oauth.BeginHandler("demo"))
			callbackHandler := sessions.Middleware(authn.Middleware(oauth.CallbackHandler("demo")))

			var cookie *http.Cookie
			states := make([]string, 2)
			for index := range states {
				var state string
				cookie, state = performOAuthBegin(t, beginHandler, cookie, "/tab/"+string(rune('a'+index)))
				states[index] = state
			}
			if got := len(oauthStatesFromCookie(t, sessions, oauth.sessionKey, cookie)); got != 2 {
				t.Fatalf("two sequential begins left %d live states, want 2", got)
			}

			for position, index := range order {
				req := httptest.NewRequest(http.MethodGet, "/auth/oauth/demo/callback?code=oauth-code&state="+url.QueryEscape(states[index]), nil)
				req.AddCookie(cookie)
				res := httptest.NewRecorder()
				callbackHandler.ServeHTTP(res, req)
				if res.Code != http.StatusSeeOther {
					t.Fatalf("callback %d status = %d, want 303: %s", index, res.Code, res.Body.String())
				}
				cookie = firstCookie(res)
				if cookie == nil {
					t.Fatalf("callback %d did not return the browser cookie", index)
				}
				if position == 0 {
					remaining := oauthStatesFromCookie(t, sessions, oauth.sessionKey, cookie)
					if len(remaining) != 1 {
						t.Fatalf("after first callback live state count = %d, want 1", len(remaining))
					}
					if _, ok := remaining[states[1-index]]; !ok {
						t.Fatalf("callback %d consumed the other tab's state", index)
					}
				}
			}
			if remaining := oauthStatesFromCookie(t, sessions, oauth.sessionKey, cookie); len(remaining) != 0 {
				t.Fatalf("after both callbacks live states = %#v, want empty", remaining)
			}
		})
	}
}

func TestOAuthUnknownStatePreservesLiveTabs(t *testing.T) {
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"token_123"}`))
	}))
	defer providerServer.Close()

	sessions := session.MustNew("oauth-unknown-state-secret", session.Options{})
	authn := New(sessions, Options{})
	oauth := authn.OAuth(OAuthOptions{
		HTTPClient: providerServer.Client(),
		Providers:  []OAuthProvider{testOAuthProvider(providerServer.URL)},
	})
	beginHandler := sessions.Middleware(oauth.BeginHandler("demo"))
	callbackHandler := sessions.Middleware(authn.Middleware(oauth.CallbackHandler("demo")))

	var cookie *http.Cookie
	states := make([]string, 2)
	for index := range states {
		var state string
		cookie, state = performOAuthBegin(t, beginHandler, cookie, "/tab/"+string(rune('a'+index)))
		states[index] = state
	}

	unknownReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/demo/callback?code=oauth-code&state=not-a-live-state", nil)
	unknownReq.AddCookie(cookie)
	unknownRes := httptest.NewRecorder()
	callbackHandler.ServeHTTP(unknownRes, unknownReq)
	if unknownRes.Code != http.StatusSeeOther {
		t.Fatalf("unknown callback status = %d, want 303", unknownRes.Code)
	}
	cookie = firstCookie(unknownRes)
	if cookie == nil {
		t.Fatal("unknown callback did not return a browser cookie")
	}
	preserved := oauthStatesFromCookie(t, sessions, oauth.sessionKey, cookie)
	if len(preserved) != 2 {
		t.Fatalf("unknown callback left %d live states, want 2", len(preserved))
	}
	for _, state := range states {
		if _, ok := preserved[state]; !ok {
			t.Fatalf("unknown callback lost live state %q", state)
		}
	}

	validReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/demo/callback?code=oauth-code&state="+url.QueryEscape(states[0]), nil)
	validReq.AddCookie(cookie)
	validRes := httptest.NewRecorder()
	callbackHandler.ServeHTTP(validRes, validReq)
	if validRes.Code != http.StatusSeeOther {
		t.Fatalf("valid callback after unknown state status = %d, want 303", validRes.Code)
	}
	remaining := oauthStatesFromCookie(t, sessions, oauth.sessionKey, firstCookie(validRes))
	if len(remaining) != 1 {
		t.Fatalf("valid callback after unknown state left %d live states, want 1", len(remaining))
	}
}

func TestOAuthConsumesBeforeProviderFailureAndReplay(t *testing.T) {
	tests := []struct {
		name             string
		tokenFailure     bool
		userinfoFailure  bool
		wantTokenCalls   int
		wantUserinfoCall int
	}{
		{name: "token exchange", tokenFailure: true, wantTokenCalls: 1},
		{name: "userinfo", userinfoFailure: true, wantTokenCalls: 1, wantUserinfoCall: 1},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			var tokenCalls, userinfoCalls int
			providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/token":
					tokenCalls++
					if testCase.tokenFailure {
						http.Error(w, "exchange failed", http.StatusBadGateway)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"access_token":"token_123"}`))
				case "/userinfo":
					userinfoCalls++
					if testCase.userinfoFailure {
						http.Error(w, "userinfo failed", http.StatusBadGateway)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"sub":"user-123","email":"ada@example.com"}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer providerServer.Close()

			sessions := session.MustNew("oauth-failure-replay-secret", session.Options{})
			authn := New(sessions, Options{})
			oauth := authn.OAuth(OAuthOptions{
				HTTPClient: providerServer.Client(),
				Providers: []OAuthProvider{{
					Name:         "demo",
					ClientID:     "client-id",
					ClientSecret: "client-secret",
					AuthorizeURL: providerServer.URL + "/authorize",
					TokenURL:     providerServer.URL + "/token",
					RedirectURL:  "http://localhost/auth/oauth/demo/callback",
					UserInfoURL:  providerServer.URL + "/userinfo",
				}},
			})
			beginHandler := sessions.Middleware(oauth.BeginHandler("demo"))
			callbackHandler := sessions.Middleware(oauth.CallbackHandler("demo"))
			cookie, state := performOAuthBegin(t, beginHandler, nil, "/after")

			callbackReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/demo/callback?code=oauth-code&state="+url.QueryEscape(state), nil)
			callbackReq.AddCookie(cookie)
			callbackRes := httptest.NewRecorder()
			callbackHandler.ServeHTTP(callbackRes, callbackReq)
			if callbackRes.Code != http.StatusSeeOther {
				t.Fatalf("provider failure status = %d, want 303", callbackRes.Code)
			}
			cookie = firstCookie(callbackRes)
			if cookie == nil {
				t.Fatal("provider failure did not return a browser cookie")
			}
			if states := oauthStatesFromCookie(t, sessions, oauth.sessionKey, cookie); len(states) != 0 {
				t.Fatalf("provider failure cookie retained consumed state: %#v", states)
			}
			if tokenCalls != testCase.wantTokenCalls || userinfoCalls != testCase.wantUserinfoCall {
				t.Fatalf("initial provider calls = token %d/userinfo %d, want token %d/userinfo %d", tokenCalls, userinfoCalls, testCase.wantTokenCalls, testCase.wantUserinfoCall)
			}

			replayReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/demo/callback?code=oauth-code&state="+url.QueryEscape(state), nil)
			replayReq.AddCookie(cookie)
			replayRes := httptest.NewRecorder()
			callbackHandler.ServeHTTP(replayRes, replayReq)
			if replayRes.Code != http.StatusSeeOther {
				t.Fatalf("replay status = %d, want 303 failure redirect", replayRes.Code)
			}
			if tokenCalls != testCase.wantTokenCalls || userinfoCalls != testCase.wantUserinfoCall {
				t.Fatalf("replay made provider calls: token %d/userinfo %d", tokenCalls, userinfoCalls)
			}
		})
	}
}

func TestOAuthExpiryBoundaryAndBeginPruning(t *testing.T) {
	start := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := start
	sessions := session.MustNew("oauth-expiry-boundary-secret", session.Options{})
	authn := New(sessions, Options{})
	oauth := authn.OAuth(OAuthOptions{
		TTL: time.Minute,
		Now: func() time.Time { return now },
		Providers: []OAuthProvider{{
			Name:         "demo",
			AuthorizeURL: "https://provider.example/authorize",
			TokenURL:     "https://provider.example/token",
			RedirectURL:  "http://localhost/auth/oauth/demo/callback",
		}},
	})
	beginHandler := sessions.Middleware(oauth.BeginHandler("demo"))
	cookie, firstState := performOAuthBegin(t, beginHandler, nil, "/first")

	// Begin at now == first expiry must prune the old entry before inserting.
	now = start.Add(time.Minute)
	cookie, secondState := performOAuthBegin(t, beginHandler, cookie, "/second")
	states := oauthStatesFromCookie(t, sessions, oauth.sessionKey, cookie)
	if len(states) != 1 {
		t.Fatalf("pruned begin left %d states, want 1", len(states))
	}
	if _, ok := states[firstState]; ok {
		t.Fatalf("expired state %q survived begin pruning", firstState)
	}
	second := states[secondState]
	if second.ExpiresAt != start.Add(2*time.Minute).UnixMilli() {
		t.Fatalf("second expiry = %d, want Unix-ms %d", second.ExpiresAt, start.Add(2*time.Minute).UnixMilli())
	}

	// Callback at now == expiry is expired, and its matched state is consumed.
	now = start.Add(2 * time.Minute)
	callback := sessions.Middleware(oauth.CallbackHandler("demo"))
	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/demo/callback?code=oauth-code&state="+url.QueryEscape(secondState), nil)
	req.AddCookie(cookie)
	res := httptest.NewRecorder()
	callback.ServeHTTP(res, req)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("expiry-boundary callback status = %d, want 303", res.Code)
	}
	if cookie = firstCookie(res); cookie == nil {
		t.Fatal("expiry-boundary callback did not return a cookie")
	}
	if states := oauthStatesFromCookie(t, sessions, oauth.sessionKey, cookie); len(states) != 0 {
		t.Fatalf("expired callback retained state: %#v", states)
	}
}

func TestOAuthStateCapEvictsOldestAndLexicalTie(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	sessions := session.MustNew("oauth-cap-eviction-secret", session.Options{})
	authn := New(sessions, Options{})
	oauth := authn.OAuth(OAuthOptions{
		Now: nowFunc(now),
		Providers: []OAuthProvider{{
			Name:         "demo",
			AuthorizeURL: "https://provider.example/authorize",
			TokenURL:     "https://provider.example/token",
			RedirectURL:  "http://localhost/auth/oauth/demo/callback",
		}},
	})
	var inserted string
	handler := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		states := map[string]oauthState{
			"z-state": {Provider: "demo", Verifier: "z", ExpiresAt: now.Add(10 * time.Minute).UnixMilli()},
			"a-state": {Provider: "demo", Verifier: "a", ExpiresAt: now.Add(10 * time.Minute).UnixMilli()},
		}
		if err := session.Current(r).Set(oauth.sessionKey, states); err != nil {
			t.Fatal(err)
		}
		var err error
		inserted, err = oauth.saveState(r, oauthState{
			Provider:  "demo",
			Verifier:  "new",
			Next:      "/new",
			ExpiresAt: now.Add(20 * time.Minute).UnixMilli(),
		})
		if err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/begin", nil))
	cookie := firstCookie(res)
	if cookie == nil {
		t.Fatal("cap test did not return a cookie")
	}
	states := oauthStatesFromCookie(t, sessions, oauth.sessionKey, cookie)
	if len(states) != oauthMaxLiveStates {
		t.Fatalf("capped state count = %d, want %d", len(states), oauthMaxLiveStates)
	}
	if _, ok := states["a-state"]; ok {
		t.Fatal("lexically older tied state a-state was not evicted")
	}
	if _, ok := states["z-state"]; !ok {
		t.Fatal("lexically newer tied state z-state was evicted")
	}
	if _, ok := states[inserted]; !ok {
		t.Fatalf("new state %q was not inserted", inserted)
	}

	encoded, err := json.Marshal(states)
	if err != nil {
		t.Fatal(err)
	}
	var records map[string]map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &records); err != nil {
		t.Fatal(err)
	}
	for key, record := range records {
		if _, ok := record["state"]; ok {
			t.Fatalf("record %q still contains the legacy duplicated state field", key)
		}
		if _, ok := record["expiresAt"]; !ok {
			t.Fatalf("record %q omitted Unix-ms expiresAt", key)
		}
	}
	if got := oldestOAuthState(map[string]oauthState{
		"later":   {ExpiresAt: now.Add(time.Minute).UnixMilli()},
		"earlier": {ExpiresAt: now.Add(-time.Minute).UnixMilli()},
	}); got != "earlier" {
		t.Fatalf("oldest expiry = %q, want earlier", got)
	}
}

func TestOAuthMalformedBeginResetsButCallbackRejects(t *testing.T) {
	sessions := session.MustNew("oauth-malformed-state-secret", session.Options{})
	authn := New(sessions, Options{})
	oauth := authn.OAuth(OAuthOptions{
		Providers: []OAuthProvider{{
			Name:         "demo",
			AuthorizeURL: "https://provider.example/authorize",
			TokenURL:     "https://provider.example/token",
			RedirectURL:  "http://localhost/auth/oauth/demo/callback",
		}},
	})

	var reset map[string]oauthState
	begin := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := session.Current(r).Set(oauth.sessionKey, "legacy-envelope"); err != nil {
			t.Fatal(err)
		}
		if _, err := oauth.Begin(r, "demo", "/"); err != nil {
			t.Fatal(err)
		}
		if !session.Current(r).Decode(oauth.sessionKey, &reset) {
			t.Fatal("Begin did not replace the malformed state value")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	begin.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/begin", nil))
	if len(reset) != 1 {
		t.Fatalf("reset map length = %d, want 1", len(reset))
	}

	var callbackErr error
	var raw any
	callback := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := session.Current(r).Set(oauth.sessionKey, "legacy-envelope"); err != nil {
			t.Fatal(err)
		}
		_, _, callbackErr = oauth.Callback(r, "demo")
		raw = session.Current(r).Value(oauth.sessionKey)
		w.WriteHeader(http.StatusNoContent)
	}))
	callback.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/callback?code=x&state=legacy", nil))
	if !errors.Is(callbackErr, ErrOAuthStateInvalid) {
		t.Fatalf("malformed callback error = %v, want ErrOAuthStateInvalid", callbackErr)
	}
	if got, ok := raw.(string); !ok || got != "legacy-envelope" {
		t.Fatalf("malformed callback rewrote stored value to %#v", raw)
	}
}

func TestOAuthProviderMismatchConsumesMatchedState(t *testing.T) {
	sessions := session.MustNew("oauth-provider-mismatch-secret", session.Options{})
	authn := New(sessions, Options{})
	oauth := authn.OAuth(OAuthOptions{
		Providers: []OAuthProvider{{
			Name:         "demo",
			AuthorizeURL: "https://provider.example/authorize",
			TokenURL:     "https://provider.example/token",
			RedirectURL:  "http://localhost/auth/oauth/demo/callback",
		}},
	})
	var callbackErr, replayErr error
	var remaining map[string]oauthState
	handler := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := session.Current(r).Set(oauth.sessionKey, map[string]oauthState{
			"known-state": {Provider: "other", Verifier: "verifier", ExpiresAt: time.Now().Add(time.Minute).UnixMilli()},
		}); err != nil {
			t.Fatal(err)
		}
		_, _, callbackErr = oauth.Callback(r, "demo")
		if session.Current(r).Decode(oauth.sessionKey, &remaining) {
			t.Fatalf("provider mismatch left state map %#v", remaining)
		}
		_, _, replayErr = oauth.Callback(r, "demo")
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/callback?code=x&state=known-state", nil)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if !errors.Is(callbackErr, ErrOAuthStateInvalid) {
		t.Fatalf("provider mismatch error = %v, want ErrOAuthStateInvalid", callbackErr)
	}
	if !errors.Is(replayErr, ErrOAuthStateInvalid) {
		t.Fatalf("provider mismatch replay error = %v, want ErrOAuthStateInvalid", replayErr)
	}
}

func TestOAuthEncryptedExactReturnTargetAndCommitFailureFailClosed(t *testing.T) {
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"token_123"}`))
	}))
	defer providerServer.Close()

	target := "/?" + strings.Repeat("a=1&", 200) + "b="
	target += strings.Repeat("x", maxReturnPathBytes-len(target))
	if len(target) != maxReturnPathBytes || !strings.Contains(target, "&") {
		t.Fatalf("test target length = %d, want %d with ampersands", len(target), maxReturnPathBytes)
	}
	if canonical, ok := SafeReturnPath(target); !ok || canonical != target {
		t.Fatalf("exact target canonicalization = (%q, %v), want unchanged accepted target", canonical, ok)
	}

	sessions := session.MustNew("oauth-encrypted-exact-secret", session.Options{Encrypt: true})
	authn := New(sessions, Options{})
	oauth := authn.OAuth(OAuthOptions{
		HTTPClient: providerServer.Client(),
		Providers:  []OAuthProvider{testOAuthProvider(providerServer.URL)},
	})
	begin := sessions.Middleware(oauth.BeginHandler("demo"))
	req := httptest.NewRequest(http.MethodGet, "/begin?next="+url.QueryEscape(target), nil)
	res := httptest.NewRecorder()
	begin.ServeHTTP(res, req)
	if res.Code != http.StatusTemporaryRedirect {
		t.Fatalf("encrypted exact begin status = %d, want 307", res.Code)
	}
	state := oauthStateFromLocation(t, res.Header().Get("Location"))
	cookie := firstCookie(res)
	if cookie == nil {
		t.Fatal("encrypted exact begin did not commit a cookie")
	}
	callback := sessions.Middleware(oauth.CallbackHandler("demo"))
	callbackReq := httptest.NewRequest(http.MethodGet, "/callback?code=x&state="+url.QueryEscape(state), nil)
	callbackReq.AddCookie(cookie)
	callbackRes := httptest.NewRecorder()
	callback.ServeHTTP(callbackRes, callbackReq)
	if callbackRes.Code != http.StatusSeeOther {
		t.Fatalf("encrypted exact callback status = %d, want 303", callbackRes.Code)
	}
	if got := callbackRes.Header().Get("Location"); got != target {
		t.Fatalf("encrypted exact callback Location length=%d, want exact target length=%d", len(got), len(target))
	}

	failingSessions := session.MustNew("oauth-commit-failure-secret", session.Options{Encrypt: true})
	failingAuthn := New(failingSessions, Options{})
	failingOAuth := failingAuthn.OAuth(OAuthOptions{
		HTTPClient: providerServer.Client(),
		Providers:  []OAuthProvider{testOAuthProvider(providerServer.URL)},
	})
	failingBegin := failingSessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := session.Current(r).Set("oversize", strings.Repeat("x", 4000)); err != nil {
			t.Fatal(err)
		}
		failingOAuth.BeginHandler("demo").ServeHTTP(w, r)
	}))
	failingBeginRes := httptest.NewRecorder()
	failingBegin.ServeHTTP(failingBeginRes, httptest.NewRequest(http.MethodGet, "/begin?next=%2Fafter", nil))
	if failingBeginRes.Code != http.StatusInternalServerError {
		t.Fatalf("oversized begin status = %d, want 500", failingBeginRes.Code)
	}
	if got := failingBeginRes.Header().Get("Location"); got != "" {
		t.Fatalf("oversized begin Location = %q, want empty", got)
	}

	// Obtain a valid callback state, then add the oversized value immediately
	// before the callback's final commit.
	normalBegin := failingSessions.Middleware(failingOAuth.BeginHandler("demo"))
	normalRes := httptest.NewRecorder()
	normalBegin.ServeHTTP(normalRes, httptest.NewRequest(http.MethodGet, "/begin?next=%2Fafter", nil))
	normalCookie := firstCookie(normalRes)
	if normalCookie == nil {
		t.Fatal("normal begin for callback failure did not commit a cookie")
	}
	normalState := oauthStateFromLocation(t, normalRes.Header().Get("Location"))
	failingCallback := failingSessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := session.Current(r).Set("oversize", strings.Repeat("x", 4000)); err != nil {
			t.Fatal(err)
		}
		failingOAuth.CallbackHandler("demo").ServeHTTP(w, r)
	}))
	failingCallbackReq := httptest.NewRequest(http.MethodGet, "/callback?code=x&state="+url.QueryEscape(normalState), nil)
	failingCallbackReq.AddCookie(normalCookie)
	failingCallbackRes := httptest.NewRecorder()
	failingCallback.ServeHTTP(failingCallbackRes, failingCallbackReq)
	if failingCallbackRes.Code != http.StatusInternalServerError {
		t.Fatalf("oversized callback status = %d, want 500", failingCallbackRes.Code)
	}
	if got := failingCallbackRes.Header().Get("Location"); got != "" {
		t.Fatalf("oversized callback Location = %q, want empty", got)
	}
}

func performOAuthBegin(t *testing.T, handler http.Handler, cookie *http.Cookie, next string) (*http.Cookie, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/demo?next="+url.QueryEscape(next), nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusTemporaryRedirect {
		t.Fatalf("begin status = %d, want 307: %s", res.Code, res.Body.String())
	}
	newCookie := firstCookie(res)
	if newCookie == nil {
		t.Fatal("begin did not return a browser cookie")
	}
	return newCookie, oauthStateFromLocation(t, res.Header().Get("Location"))
}

func oauthStateFromLocation(t *testing.T, location string) string {
	t.Helper()
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse OAuth location %q: %v", location, err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatalf("OAuth location %q has no state", location)
	}
	return state
}

func oauthStatesFromCookie(t *testing.T, sessions *session.Manager, key string, cookie *http.Cookie) map[string]oauthState {
	t.Helper()
	if cookie == nil {
		return nil
	}
	var states map[string]oauthState
	handler := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !session.Current(r).Decode(key, &states) {
			states = nil
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/inspect", nil)
	req.AddCookie(cookie)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("inspect status = %d, want 204", res.Code)
	}
	return states
}

func testOAuthProvider(serverURL string) OAuthProvider {
	return OAuthProvider{
		Name:         "demo",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		AuthorizeURL: serverURL + "/authorize",
		TokenURL:     serverURL + "/token",
		RedirectURL:  "http://localhost/auth/oauth/demo/callback",
		UserInfoURL:  serverURL + "/userinfo",
		Resolver: OAuthUserResolverFunc(func(context.Context, OAuthProvider, *http.Client, OAuthToken) (User, error) {
			return User{ID: "user-123", Email: "ada@example.com", Name: "Ada"}, nil
		}),
	}
}

func nowFunc(now time.Time) func() time.Time {
	return func() time.Time { return now }
}
