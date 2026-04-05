package redis

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/odvcencio/gosx/auth"
	"github.com/odvcencio/gosx/session"
	goredis "github.com/redis/go-redis/v9"
)

func TestRedisMagicLinkStoreSupportsCallbackFlow(t *testing.T) {
	mini := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer client.Close()

	sessions := session.MustNew("redis-magic-link-secret", session.Options{})
	authn := auth.New(sessions, auth.Options{LoginPath: "/login"})
	store := NewMagicLinkStore(client, Options{Prefix: "gosx:test"})

	var delivered auth.MagicLinkDelivery
	magic := authn.MagicLinks(auth.MagicLinkOptions{
		SuccessPath: "/welcome",
		Store:       store,
		Sender: auth.MagicLinkSenderFunc(func(ctx context.Context, delivery auth.MagicLinkDelivery) error {
			delivered = delivery
			return nil
		}),
	})

	requestHandler := sessions.Middleware(magic.RequestHandler())
	callbackHandler := sessions.Middleware(authn.Middleware(magic.CallbackHandler()))

	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/request", bytes.NewBufferString(`{"email":"ada@example.com","next":"/admin"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res := httptest.NewRecorder()
	requestHandler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if delivered.Token == "" {
		t.Fatal("expected delivered magic-link token")
	}

	callbackReq := httptest.NewRequest(http.MethodGet, "/auth/magic-link?token="+delivered.Token, nil)
	callbackRes := httptest.NewRecorder()
	callbackHandler.ServeHTTP(callbackRes, callbackReq)
	if callbackRes.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", callbackRes.Code)
	}
	if callbackRes.Header().Get("Location") != "/admin" {
		t.Fatalf("expected redirect to /admin, got %q", callbackRes.Header().Get("Location"))
	}

	if _, err := store.Consume(delivered.Token, delivered.ExpiresAt.Add(-time.Minute)); err != auth.ErrMagicLinkInvalid {
		t.Fatalf("expected token to be consumed once, got %v", err)
	}
}

func TestRedisWebAuthnStoreSupportsRegistrationAndLoginFlow(t *testing.T) {
	mini := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	defer client.Close()

	sessions := session.MustNew("redis-webauthn-secret", session.Options{})
	authn := auth.New(sessions, auth.Options{LoginPath: "/login"})
	store := NewWebAuthnStore(client, Options{Prefix: "gosx:test"})
	webauthn := authn.WebAuthn(auth.WebAuthnOptions{
		RPID:   "localhost",
		RPName: "GoSX Test",
		Origin: "http://localhost:8080",
		Store:  store,
	})

	registerOptions := sessions.Middleware(webauthn.RegisterOptionsHandler())
	registerFinish := sessions.Middleware(authn.Middleware(webauthn.RegisterHandler()))
	loginOptions := sessions.Middleware(webauthn.LoginOptionsHandler())
	loginFinish := sessions.Middleware(authn.Middleware(webauthn.LoginHandler()))

	registerReq := httptest.NewRequest(http.MethodPost, "/auth/webauthn/register/options", bytes.NewBufferString(`{"user":{"id":"user_ada","email":"ada@example.com","name":"Ada"}}`))
	registerReq.Header.Set("Content-Type", "application/json")
	registerReq.Header.Set("Accept", "application/json")
	registerRes := httptest.NewRecorder()
	registerOptions.ServeHTTP(registerRes, registerReq)
	if registerRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", registerRes.Code)
	}
	var registerOptionsPayload struct {
		Options auth.WebAuthnCreationOptions `json:"options"`
	}
	if err := json.Unmarshal(registerRes.Body.Bytes(), &registerOptionsPayload); err != nil {
		t.Fatalf("decode register options: %v", err)
	}
	registerCookie := firstCookie(registerRes)
	if registerCookie == nil {
		t.Fatal("expected registration cookie")
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	rawID := mustRandomBytes(t, 32)
	registerPayload := auth.WebAuthnRegistrationResponse{
		ID:    encodeWebAuthnBytes(rawID),
		RawID: encodeWebAuthnBytes(rawID),
		Type:  "public-key",
	}
	registerPayload.Response.ClientDataJSON = encodeWebAuthnBytes(mustJSON(t, map[string]any{
		"type":      "webauthn.create",
		"challenge": registerOptionsPayload.Options.Challenge,
		"origin":    "http://localhost:8080",
	}))
	registerPayload.Response.AuthenticatorData = encodeWebAuthnBytes(webAuthnAuthData("localhost", 0x45, 0))
	registerPayload.Response.PublicKey = encodeWebAuthnBytes(publicKeyDER)
	registerPayload.Response.PublicKeyAlgorithm = -7
	registerPayload.Response.Transports = []string{"internal"}

	finishRegisterReq := httptest.NewRequest(http.MethodPost, "/auth/webauthn/register", bytes.NewBuffer(mustJSON(t, registerPayload)))
	finishRegisterReq.AddCookie(registerCookie)
	finishRegisterReq.Header.Set("Content-Type", "application/json")
	finishRegisterReq.Header.Set("Accept", "application/json")
	finishRegisterRes := httptest.NewRecorder()
	registerFinish.ServeHTTP(finishRegisterRes, finishRegisterReq)
	if finishRegisterRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", finishRegisterRes.Code, finishRegisterRes.Body.String())
	}

	credential, err := store.Credential(encodeWebAuthnBytes(rawID))
	if err != nil {
		t.Fatalf("expected stored credential: %v", err)
	}
	if credential.User.Email != "ada@example.com" {
		t.Fatalf("unexpected stored credential %+v", credential)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/auth/webauthn/login/options", bytes.NewBufferString(`{"next":"/admin"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("Accept", "application/json")
	loginRes := httptest.NewRecorder()
	loginOptions.ServeHTTP(loginRes, loginReq)
	if loginRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", loginRes.Code)
	}
	var loginOptionsPayload struct {
		Options auth.WebAuthnRequestOptions `json:"options"`
	}
	if err := json.Unmarshal(loginRes.Body.Bytes(), &loginOptionsPayload); err != nil {
		t.Fatalf("decode login options: %v", err)
	}
	loginCookie := firstCookie(loginRes)
	if loginCookie == nil {
		t.Fatal("expected login cookie")
	}

	clientDataJSON := mustJSON(t, map[string]any{
		"type":      "webauthn.get",
		"challenge": loginOptionsPayload.Options.Challenge,
		"origin":    "http://localhost:8080",
	})
	authData := webAuthnAuthData("localhost", 0x05, 1)
	signature := signAssertion(t, privateKey, authData, clientDataJSON)
	loginPayload := auth.WebAuthnAuthenticationResponse{
		ID:    encodeWebAuthnBytes(rawID),
		RawID: encodeWebAuthnBytes(rawID),
		Type:  "public-key",
	}
	loginPayload.Response.ClientDataJSON = encodeWebAuthnBytes(clientDataJSON)
	loginPayload.Response.AuthenticatorData = encodeWebAuthnBytes(authData)
	loginPayload.Response.Signature = encodeWebAuthnBytes(signature)

	finishLoginReq := httptest.NewRequest(http.MethodPost, "/auth/webauthn/login", bytes.NewBuffer(mustJSON(t, loginPayload)))
	finishLoginReq.AddCookie(loginCookie)
	finishLoginReq.Header.Set("Content-Type", "application/json")
	finishLoginReq.Header.Set("Accept", "application/json")
	finishLoginRes := httptest.NewRecorder()
	loginFinish.ServeHTTP(finishLoginRes, finishLoginReq)
	if finishLoginRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", finishLoginRes.Code, finishLoginRes.Body.String())
	}

	updated, err := store.Credential(encodeWebAuthnBytes(rawID))
	if err != nil {
		t.Fatalf("reload credential: %v", err)
	}
	if updated.SignCount != 1 {
		t.Fatalf("expected updated sign count, got %+v", updated)
	}
}

func firstCookie(recorder *httptest.ResponseRecorder) *http.Cookie {
	result := recorder.Result()
	defer result.Body.Close()
	for _, cookie := range result.Cookies() {
		if cookie.Name != "" {
			return cookie
		}
	}
	return nil
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func mustRandomBytes(t *testing.T, size int) []byte {
	t.Helper()
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	return buf
}

func encodeWebAuthnBytes(value []byte) string {
	if len(value) == 0 {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(value)
}

func webAuthnAuthData(rpID string, flags byte, signCount uint32) []byte {
	hash := sha256.Sum256([]byte(rpID))
	out := make([]byte, 37)
	copy(out[:32], hash[:])
	out[32] = flags
	out[33] = byte(signCount >> 24)
	out[34] = byte(signCount >> 16)
	out[35] = byte(signCount >> 8)
	out[36] = byte(signCount)
	return out
}

func signAssertion(t *testing.T, privateKey *ecdsa.PrivateKey, authData, clientDataJSON []byte) []byte {
	t.Helper()
	clientHash := sha256.Sum256(clientDataJSON)
	message := append(append([]byte(nil), authData...), clientHash[:]...)
	digest := sha256.Sum256(message)
	signature, err := ecdsa.SignASN1(rand.Reader, privateKey, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signature
}
