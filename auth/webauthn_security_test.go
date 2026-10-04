package auth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/session"
)

func TestWebAuthnAnonymousRegistrationFailsClosed(t *testing.T) {
	for _, contentType := range []string{"application/json", "application/x-www-form-urlencoded"} {
		t.Run(contentType, func(t *testing.T) {
			sessions := session.MustNew("registration-security-secret", session.Options{})
			authn := New(sessions, Options{})
			passkeys := authn.WebAuthn(WebAuthnOptions{Origin: "https://app.example"})
			body := `{"user":{"id":"admin","roles":["admin"],"meta":{"privileged":true}}}`
			if contentType != "application/json" {
				body = "id=admin&email=admin%40app.example&roles=admin"
			}
			req := httptest.NewRequest(http.MethodPost, "/register/options", strings.NewReader(body))
			req.Header.Set("Content-Type", contentType)
			res := httptest.NewRecorder()
			sessions.Middleware(authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				passkeys.RegisterOptionsHandler().ServeHTTP(w, r)
				var state webAuthnState
				if session.Current(r).Decode(passkeys.sessionKey, &state) {
					t.Fatal("anonymous request created registration state")
				}
				var user User
				if session.Current(r).Decode(authn.sessionKey, &user) {
					t.Fatal("anonymous request signed in")
				}
			}))).ServeHTTP(res, req)
			if res.Code != http.StatusUnauthorized {
				t.Fatalf("registration status = %d, want 401: %s", res.Code, res.Body.String())
			}
		})
	}
}

func TestWebAuthnRegistrationIgnoresClientIdentity(t *testing.T) {
	sessions := session.MustNew("registration-identity-secret", session.Options{})
	authn := New(sessions, Options{})
	trusted := User{ID: "ada", Email: "ada@app.example", Name: "Ada", Roles: []string{"member"}, Meta: map[string]any{"source": "server"}}
	passkeys := authn.WebAuthn(WebAuthnOptions{
		Origin: "https://app.example",
		RegistrationUser: func(*http.Request) (User, error) {
			t.Fatal("sign-up callback ran for an authenticated user")
			return User{}, nil
		},
	})
	cookie := webAuthnSignInCookie(t, sessions, authn, trusted)
	options, cookie := beginWebAuthnEnrollment(t, sessions, authn, passkeys, cookie)
	if options.User.ID != encodeWebAuthnBytes([]byte(trusted.ID)) || options.User.Name != trusted.Email {
		t.Fatalf("registration used client identity: %+v", options.User)
	}
	if options.AuthenticatorSelection.ResidentKey != "required" {
		t.Fatal("registration must require a discoverable credential")
	}
	payload := webAuthnRegistrationPayload(t, options.Challenge)
	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(mustJSONBytes(t, payload)))
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	sessions.Middleware(authn.Middleware(passkeys.RegisterHandler())).ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("registration status = %d: %s", res.Code, res.Body.String())
	}
	credential, err := passkeys.store.Credential(payload.ID)
	if err != nil || !reflect.DeepEqual(credential.User, trusted) {
		t.Fatalf("stored identity = %+v, err = %v; want %+v", credential.User, err, trusted)
	}
}

func TestWebAuthnSignUpCallbackRequiresServerAuthorization(t *testing.T) {
	for _, test := range []struct {
		name string
		user User
		err  error
	}{
		{name: "authorized", user: User{ID: "new-server-user", Roles: []string{"member"}}},
		{name: "denied", user: User{ID: "admin"}, err: errors.New("enrollment denied")},
		{name: "empty identity"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sessions := session.MustNew("signup-security-secret", session.Options{})
			authn := New(sessions, Options{})
			passkeys := authn.WebAuthn(WebAuthnOptions{
				Origin:           "https://app.example",
				RegistrationUser: func(*http.Request) (User, error) { return test.user, test.err },
			})
			if test.err != nil || test.user.ID == "" {
				req := httptest.NewRequest(http.MethodPost, "/register/options", strings.NewReader(`{"user":{"id":"admin","roles":["admin"]}}`))
				req.Header.Set("Content-Type", "application/json")
				res := httptest.NewRecorder()
				sessions.Middleware(authn.Middleware(passkeys.RegisterOptionsHandler())).ServeHTTP(res, req)
				if res.Code != http.StatusUnauthorized {
					t.Fatalf("denied enrollment status = %d, want 401", res.Code)
				}
				return
			}
			options, cookie := beginWebAuthnEnrollment(t, sessions, authn, passkeys, nil)
			payload := webAuthnRegistrationPayload(t, options.Challenge)
			req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(mustJSONBytes(t, payload)))
			req.AddCookie(cookie)
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			sessions.Middleware(authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				passkeys.RegisterHandler().ServeHTTP(w, r)
				var user User
				if session.Current(r).Decode(authn.sessionKey, &user) {
					t.Fatalf("registration signed in an anonymous user: %+v", user)
				}
			}))).ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("authorized enrollment status = %d: %s", res.Code, res.Body.String())
			}
			credential, err := passkeys.store.Credential(payload.ID)
			if err != nil || !reflect.DeepEqual(credential.User, test.user) {
				t.Fatalf("callback identity = %+v, err = %v", credential.User, err)
			}
		})
	}
}

func TestWebAuthnRegistrationPreservesCurrentIdentityAndPrivileges(t *testing.T) {
	for _, test := range []struct {
		name string
		user User
		want error
	}{
		{name: "same user with reduced privileges", user: User{ID: "ada", Roles: []string{"member"}, Meta: map[string]any{"privileged": false}}},
		{name: "different user", user: User{ID: "other"}, want: ErrWebAuthnRegistrationUnauthorized},
		{name: "signed out", want: ErrWebAuthnRegistrationUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			sessions := session.MustNew("registration-session-secret", session.Options{})
			authn := New(sessions, Options{})
			passkeys := authn.WebAuthn(WebAuthnOptions{Origin: "https://app.example"})
			initial := webAuthnSignInCookie(t, sessions, authn, User{ID: "ada", Roles: []string{"admin"}, Meta: map[string]any{"privileged": true}})
			options, cookie := beginWebAuthnEnrollment(t, sessions, authn, passkeys, initial)
			payload := webAuthnRegistrationPayload(t, options.Challenge)
			req := httptest.NewRequest(http.MethodPost, "/register", nil)
			req.AddCookie(cookie)
			sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				store := session.Current(r)
				store.Delete(authn.sessionKey)
				if test.user.ID != "" {
					store.Set(authn.sessionKey, test.user)
				}
				authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					credential, _, err := passkeys.FinishRegistration(r, payload)
					if !errors.Is(err, test.want) {
						t.Fatalf("finish error = %v, want %v", err, test.want)
					}
					var current User
					store.Decode(authn.sessionKey, &current)
					if !reflect.DeepEqual(current, test.user) {
						t.Fatalf("registration changed session identity: %+v, want %+v", current, test.user)
					}
					if test.want == nil && !reflect.DeepEqual(credential.User, test.user) {
						t.Fatalf("registration stored stale privileges: %+v", credential.User)
					}
					if test.want != nil {
						if _, err := passkeys.store.Credential(payload.ID); !errors.Is(err, ErrWebAuthnCredentialNotFound) {
							t.Fatalf("unauthorized registration stored credential: %v", err)
						}
					}
					w.WriteHeader(http.StatusNoContent)
				})).ServeHTTP(w, r)
			})).ServeHTTP(httptest.NewRecorder(), req)
		})
	}
}

func TestWebAuthnLoginOptionsProtectCredentialIDs(t *testing.T) {
	for _, test := range []struct {
		name    string
		login   string
		signed  bool
		allowed bool
	}{
		{name: "anonymous named account", login: "ada"},
		{name: "anonymous unknown account", login: "missing"},
		{name: "anonymous discoverable"},
		{name: "signed-in own account", login: "ada", signed: true, allowed: true},
		{name: "signed-in default account", signed: true, allowed: true},
		{name: "signed-in another account", login: "other", signed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			sessions := session.MustNew("login-options-security-secret", session.Options{})
			authn := New(sessions, Options{})
			store := NewMemoryWebAuthnStore()
			for _, id := range []string{"ada", "other"} {
				if err := store.SaveCredential(WebAuthnCredential{ID: id + "-private-credential", User: User{ID: id}}); err != nil {
					t.Fatal(err)
				}
			}
			passkeys := authn.WebAuthn(WebAuthnOptions{
				Origin: "https://app.example", Store: store,
				Resolver: WebAuthnResolverFunc(func(_ context.Context, login string) (User, error) {
					if !test.signed {
						t.Fatal("anonymous login resolved a client-supplied identity")
					}
					return User{ID: login}, nil
				}),
			})
			req := httptest.NewRequest(http.MethodPost, "/login/options", bytes.NewReader(mustJSONBytes(t, map[string]string{"login": test.login})))
			req.Header.Set("Content-Type", "application/json")
			if test.signed {
				req.AddCookie(webAuthnSignInCookie(t, sessions, authn, User{ID: "ada"}))
			}
			res := httptest.NewRecorder()
			sessions.Middleware(authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				passkeys.LoginOptionsHandler().ServeHTTP(w, r)
				var state webAuthnState
				if !session.Current(r).Decode(passkeys.sessionKey, &state) {
					t.Fatal("missing login state")
				}
				if !test.allowed && len(state.Allowed) != 0 {
					t.Fatalf("credential IDs leaked in session state: %+v", state.Allowed)
				}
				if !test.signed && state.User.ID != "" {
					t.Fatal("anonymous login hint bound a server identity")
				}
			}))).ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("login options status = %d: %s", res.Code, res.Body.String())
			}
			var result struct {
				Options WebAuthnRequestOptions `json:"options"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if test.allowed {
				if len(result.Options.AllowCredentials) != 1 || result.Options.AllowCredentials[0].ID != "ada-private-credential" {
					t.Fatalf("own credentials missing: %+v", result.Options.AllowCredentials)
				}
			} else if len(result.Options.AllowCredentials) != 0 || strings.Contains(res.Body.String(), "private-credential") {
				t.Fatalf("login options exposed credential IDs: %s", res.Body.String())
			}
		})
	}
}

func TestMemoryWebAuthnStoreRejectsCredentialOverwrite(t *testing.T) {
	store := NewMemoryWebAuthnStore()
	original := WebAuthnCredential{ID: "credential", User: User{ID: "ada"}, PublicKey: []byte("original key"), SignCount: 12}
	if err := store.SaveCredential(original); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"ada", "other"} {
		replacement := WebAuthnCredential{ID: " credential ", User: User{ID: owner, Roles: []string{"admin"}}, PublicKey: []byte("replacement key")}
		if err := store.SaveCredential(replacement); !errors.Is(err, ErrWebAuthnCredentialExists) {
			t.Fatalf("duplicate credential error = %v", err)
		}
	}
	got, err := store.Credential(original.ID)
	if err != nil || !reflect.DeepEqual(got, original) {
		t.Fatalf("credential overwritten: %+v, err = %v", got, err)
	}
	owned, err := store.Credentials("ada")
	if err != nil || len(owned) != 1 {
		t.Fatalf("original index changed: %+v, err = %v", owned, err)
	}
	other, err := store.Credentials("other")
	if err != nil || len(other) != 0 {
		t.Fatalf("duplicate added another owner: %+v, err = %v", other, err)
	}
}

func TestWebAuthnRejectsLegacyRegistrationState(t *testing.T) {
	sessions := session.MustNew("legacy-registration-secret", session.Options{})
	authn := New(sessions, Options{})
	passkeys := authn.WebAuthn(WebAuthnOptions{Origin: "https://app.example"})
	sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := passkeys.saveState(r, webAuthnState{
			Kind: "register", Challenge: "legacy-challenge", User: User{ID: "admin", Roles: []string{"admin"}},
			ExpiresAt: passkeys.now().Add(passkeys.ttl),
		}); err != nil {
			t.Fatal(err)
		}
		payload := webAuthnRegistrationPayload(t, "legacy-challenge")
		if _, _, err := passkeys.FinishRegistration(r, payload); !errors.Is(err, ErrWebAuthnChallengeInvalid) {
			t.Fatalf("legacy registration error = %v, want ErrWebAuthnChallengeInvalid", err)
		}
		if _, err := passkeys.store.Credential(payload.ID); !errors.Is(err, ErrWebAuthnCredentialNotFound) {
			t.Fatalf("legacy registration stored credential: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/register", nil))
}

func TestWebAuthnFinishRegistrationRejectsCredentialOverwrite(t *testing.T) {
	sessions := session.MustNew("registration-overwrite-secret", session.Options{})
	authn := New(sessions, Options{})
	passkeys := authn.WebAuthn(WebAuthnOptions{Origin: "https://app.example"})
	initial := webAuthnSignInCookie(t, sessions, authn, User{ID: "ada"})
	options, cookie := beginWebAuthnEnrollment(t, sessions, authn, passkeys, initial)
	payload := webAuthnRegistrationPayload(t, options.Challenge)
	original := WebAuthnCredential{ID: payload.ID, User: User{ID: "other"}, PublicKey: []byte("original key"), SignCount: 12}
	if err := passkeys.store.SaveCredential(original); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/register", nil)
	req.AddCookie(cookie)
	sessions.Middleware(authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := passkeys.FinishRegistration(r, payload); !errors.Is(err, ErrWebAuthnCredentialExists) {
			t.Fatalf("duplicate registration error = %v", err)
		}
		got, err := passkeys.store.Credential(payload.ID)
		if err != nil || !reflect.DeepEqual(got, original) {
			t.Fatalf("duplicate registration replaced credential: %+v, %v", got, err)
		}
		var current User
		if !session.Current(r).Decode(authn.sessionKey, &current) || current.ID != "ada" {
			t.Fatalf("duplicate registration changed identity: %+v", current)
		}
		w.WriteHeader(http.StatusNoContent)
	}))).ServeHTTP(httptest.NewRecorder(), req)
}

func beginWebAuthnEnrollment(t *testing.T, sessions *session.Manager, authn *Manager, passkeys *WebAuthn, cookie *http.Cookie) (WebAuthnCreationOptions, *http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/register/options", strings.NewReader(`{"user":{"id":"admin","email":"admin@app.example","name":"Admin","roles":["admin"],"meta":{"privileged":true}}}`))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	sessions.Middleware(authn.Middleware(passkeys.RegisterOptionsHandler())).ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("begin registration = %d: %s", res.Code, res.Body.String())
	}
	var result struct {
		Options WebAuthnCreationOptions `json:"options"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	cookie = firstCookie(res)
	if cookie == nil {
		t.Fatal("missing registration state cookie")
	}
	return result.Options, cookie
}

func webAuthnRegistrationPayload(t *testing.T, challenge string) WebAuthnRegistrationResponse {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	id := encodeWebAuthnBytes(mustRandomBytes(t, 32))
	payload := WebAuthnRegistrationResponse{ID: id, RawID: id, Type: "public-key"}
	payload.Response.ClientDataJSON = encodeWebAuthnBytes(mustJSONBytes(t, webAuthnClientData{Type: "webauthn.create", Challenge: challenge, Origin: "https://app.example"}))
	payload.Response.AuthenticatorData = encodeWebAuthnBytes(webAuthnAuthData("app.example", 0x45, 0))
	payload.Response.PublicKey = encodeWebAuthnBytes(publicKey)
	payload.Response.PublicKeyAlgorithm = -7
	return payload
}
