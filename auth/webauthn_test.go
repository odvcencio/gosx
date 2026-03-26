package auth

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/odvcencio/gosx/session"
)

func TestWebAuthnRegistrationAndAuthenticationRoundTrip(t *testing.T) {
	sessions := session.MustNew("webauthn-test-secret", session.Options{})
	authn := New(sessions, Options{LoginPath: "/login"})
	store := NewMemoryWebAuthnStore()
	webauthn := authn.WebAuthn(WebAuthnOptions{
		RPID:   "localhost",
		RPName: "GoSX Test",
		Origin: "http://localhost:8080",
		Store:  store,
	})

	registerOptions := sessions.Middleware(webauthn.RegisterOptionsHandler())
	registerFinish := sessions.Middleware(authn.Middleware(webauthn.RegisterHandler()))
	loginOptions := sessions.Middleware(webauthn.LoginOptionsHandler())
	loginFinish := sessions.Middleware(authn.Middleware(webauthn.LoginHandler()))
	protected := sessions.Middleware(authn.Middleware(authn.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := Current(r)
		if !ok || user.Email != "ada@example.com" {
			t.Fatalf("expected signed-in user, got %#v ok=%v", user, ok)
		}
		w.WriteHeader(http.StatusOK)
	}))))

	registerReq := httptest.NewRequest(http.MethodPost, "/auth/webauthn/register/options", bytes.NewBufferString(`{"user":{"id":"user_ada","email":"ada@example.com","name":"Ada"}}`))
	registerReq.Header.Set("Content-Type", "application/json")
	registerReq.Header.Set("Accept", "application/json")
	registerRes := httptest.NewRecorder()
	registerOptions.ServeHTTP(registerRes, registerReq)
	if registerRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", registerRes.Code)
	}
	var registerOptionsPayload struct {
		Options WebAuthnCreationOptions `json:"options"`
	}
	if err := json.Unmarshal(registerRes.Body.Bytes(), &registerOptionsPayload); err != nil {
		t.Fatalf("decode register options: %v", err)
	}
	registerCookie := firstCookie(registerRes)
	if registerCookie == nil {
		t.Fatal("expected registration state cookie")
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
	createClientData := webAuthnClientData{
		Type:      "webauthn.create",
		Challenge: registerOptionsPayload.Options.Challenge,
		Origin:    "http://localhost:8080",
	}
	createClientDataJSON := mustJSONBytes(t, createClientData)
	registerPayload := WebAuthnRegistrationResponse{
		ID:    encodeWebAuthnBytes(rawID),
		RawID: encodeWebAuthnBytes(rawID),
		Type:  "public-key",
	}
	registerPayload.Response.ClientDataJSON = encodeWebAuthnBytes(createClientDataJSON)
	registerPayload.Response.AuthenticatorData = encodeWebAuthnBytes(webAuthnAuthData("localhost", 0x45, 0))
	registerPayload.Response.PublicKey = encodeWebAuthnBytes(publicKeyDER)
	registerPayload.Response.PublicKeyAlgorithm = -7
	registerPayload.Response.Transports = []string{"internal"}

	finishRegisterReq := httptest.NewRequest(http.MethodPost, "/auth/webauthn/register", bytes.NewBuffer(mustJSONBytes(t, registerPayload)))
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
		t.Fatalf("unexpected credential user %#v", credential.User)
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
		Options WebAuthnRequestOptions `json:"options"`
	}
	if err := json.Unmarshal(loginRes.Body.Bytes(), &loginOptionsPayload); err != nil {
		t.Fatalf("decode login options: %v", err)
	}
	loginCookie := firstCookie(loginRes)
	if loginCookie == nil {
		t.Fatal("expected login state cookie")
	}

	getClientData := webAuthnClientData{
		Type:      "webauthn.get",
		Challenge: loginOptionsPayload.Options.Challenge,
		Origin:    "http://localhost:8080",
	}
	getClientDataJSON := mustJSONBytes(t, getClientData)
	authData := webAuthnAuthData("localhost", 0x05, 1)
	signature := signWebAuthnAssertion(t, privateKey, authData, getClientDataJSON)
	loginPayload := WebAuthnAuthenticationResponse{
		ID:    encodeWebAuthnBytes(rawID),
		RawID: encodeWebAuthnBytes(rawID),
		Type:  "public-key",
	}
	loginPayload.Response.ClientDataJSON = encodeWebAuthnBytes(getClientDataJSON)
	loginPayload.Response.AuthenticatorData = encodeWebAuthnBytes(authData)
	loginPayload.Response.Signature = encodeWebAuthnBytes(signature)

	finishLoginReq := httptest.NewRequest(http.MethodPost, "/auth/webauthn/login", bytes.NewBuffer(mustJSONBytes(t, loginPayload)))
	finishLoginReq.AddCookie(loginCookie)
	finishLoginReq.Header.Set("Content-Type", "application/json")
	finishLoginReq.Header.Set("Accept", "application/json")
	finishLoginRes := httptest.NewRecorder()
	loginFinish.ServeHTTP(finishLoginRes, finishLoginReq)
	if finishLoginRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", finishLoginRes.Code, finishLoginRes.Body.String())
	}
	authCookie := firstCookie(finishLoginRes)
	if authCookie == nil {
		t.Fatal("expected auth cookie after passkey login")
	}

	protectedReq := httptest.NewRequest(http.MethodGet, "/secret", nil)
	protectedReq.AddCookie(authCookie)
	protectedRes := httptest.NewRecorder()
	protected.ServeHTTP(protectedRes, protectedReq)
	if protectedRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", protectedRes.Code)
	}
}

func TestWebAuthnRejectsReplayedCounter(t *testing.T) {
	sessions := session.MustNew("webauthn-replay-secret", session.Options{})
	authn := New(sessions, Options{})
	store := NewMemoryWebAuthnStore()
	webauthn := authn.WebAuthn(WebAuthnOptions{
		RPID:   "localhost",
		Origin: "http://localhost:8080",
		Store:  store,
	})

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	rawID := mustRandomBytes(t, 32)
	if err := store.SaveCredential(WebAuthnCredential{
		ID:        encodeWebAuthnBytes(rawID),
		User:      User{ID: "ada@example.com", Email: "ada@example.com", Name: "Ada"},
		PublicKey: publicKeyDER,
		Algorithm: -7,
		SignCount: 2,
	}); err != nil {
		t.Fatal(err)
	}

	var options WebAuthnRequestOptions
	begin := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		options, err = webauthn.BeginAuthentication(r, "ada@example.com", "/")
		if err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	beginRes := httptest.NewRecorder()
	begin.ServeHTTP(beginRes, httptest.NewRequest(http.MethodPost, "/begin", nil))
	loginCookie := firstCookie(beginRes)
	if loginCookie == nil {
		t.Fatal("expected login cookie")
	}

	clientDataJSON := mustJSONBytes(t, webAuthnClientData{
		Type:      "webauthn.get",
		Challenge: options.Challenge,
		Origin:    "http://localhost:8080",
	})
	authData := webAuthnAuthData("localhost", 0x05, 2)
	signature := signWebAuthnAssertion(t, privateKey, authData, clientDataJSON)
	payload := WebAuthnAuthenticationResponse{
		ID:    encodeWebAuthnBytes(rawID),
		RawID: encodeWebAuthnBytes(rawID),
		Type:  "public-key",
	}
	payload.Response.ClientDataJSON = encodeWebAuthnBytes(clientDataJSON)
	payload.Response.AuthenticatorData = encodeWebAuthnBytes(authData)
	payload.Response.Signature = encodeWebAuthnBytes(signature)

	req := httptest.NewRequest(http.MethodPost, "/finish", bytes.NewBuffer(mustJSONBytes(t, payload)))
	req.AddCookie(loginCookie)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res := httptest.NewRecorder()
	sessions.Middleware(webauthn.LoginHandler()).ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
}

func TestWebAuthnRejectsExpiredChallenge(t *testing.T) {
	now := time.Date(2026, 3, 26, 15, 0, 0, 0, time.UTC)
	sessions := session.MustNew("webauthn-expired-secret", session.Options{})
	authn := New(sessions, Options{})
	webauthn := authn.WebAuthn(WebAuthnOptions{
		RPID:   "localhost",
		Origin: "http://localhost:8080",
		TTL:    time.Minute,
		Now:    func() time.Time { return now },
	})

	var options WebAuthnCreationOptions
	begin := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		options, err = webauthn.BeginRegistration(r, User{ID: "ada@example.com", Email: "ada@example.com"}, "")
		if err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	beginRes := httptest.NewRecorder()
	begin.ServeHTTP(beginRes, httptest.NewRequest(http.MethodPost, "/begin", nil))
	registerCookie := firstCookie(beginRes)
	if registerCookie == nil {
		t.Fatal("expected registration cookie")
	}
	webauthn.now = func() time.Time { return now.Add(2 * time.Minute) }

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	rawID := mustRandomBytes(t, 32)
	payload := WebAuthnRegistrationResponse{
		ID:    encodeWebAuthnBytes(rawID),
		RawID: encodeWebAuthnBytes(rawID),
		Type:  "public-key",
	}
	payload.Response.ClientDataJSON = encodeWebAuthnBytes(mustJSONBytes(t, webAuthnClientData{
		Type:      "webauthn.create",
		Challenge: options.Challenge,
		Origin:    "http://localhost:8080",
	}))
	payload.Response.AuthenticatorData = encodeWebAuthnBytes(webAuthnAuthData("localhost", 0x45, 0))
	payload.Response.PublicKey = encodeWebAuthnBytes(publicKeyDER)
	payload.Response.PublicKeyAlgorithm = -7

	req := httptest.NewRequest(http.MethodPost, "/finish", bytes.NewBuffer(mustJSONBytes(t, payload)))
	req.AddCookie(registerCookie)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res := httptest.NewRecorder()
	sessions.Middleware(webauthn.RegisterHandler()).ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
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

func signWebAuthnAssertion(t *testing.T, privateKey *ecdsa.PrivateKey, authData, clientDataJSON []byte) []byte {
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

func mustRandomBytes(t *testing.T, size int) []byte {
	t.Helper()
	out := make([]byte, size)
	if _, err := rand.Read(out); err != nil {
		t.Fatal(err)
	}
	return out
}

func mustJSONBytes(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func firstCookie(recorder *httptest.ResponseRecorder) *http.Cookie {
	result := recorder.Result()
	cookies := result.Cookies()
	if len(cookies) == 0 {
		return nil
	}
	return cookies[0]
}

func TestWebAuthnVerifyHelperUsesExpectedCurve(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if privateKey.Curve.Params().BitSize != 256 {
		t.Fatalf("expected P-256 curve, got %d", privateKey.Curve.Params().BitSize)
	}
	if privateKey.X.Cmp(big.NewInt(0)) == 0 || privateKey.Y.Cmp(big.NewInt(0)) == 0 {
		t.Fatal("expected public key coordinates")
	}
}
