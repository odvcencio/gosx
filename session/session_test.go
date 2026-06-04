package session

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failingRandReader struct{}

func (failingRandReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func TestMustNewInvalidSecretReturnsNilManager(t *testing.T) {
	manager := MustNew("short", Options{})
	if manager != nil {
		t.Fatal("expected nil manager for invalid secret")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	manager.Middleware(nil).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("nil manager middleware status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestMiddlewarePersistsValuesAndFlashes(t *testing.T) {
	manager := MustNew("session-test-secret-value", Options{})
	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store := Current(r)
		switch r.URL.Path {
		case "/set":
			store.Set("team", "platform")
			store.AddFlash("notice", "saved")
			w.WriteHeader(http.StatusNoContent)
		case "/read":
			if got := store.String("team"); got != "platform" {
				t.Fatalf("expected session value, got %q", got)
			}
			flashes := store.Flashes("notice")
			if len(flashes) != 1 || flashes[0] != "saved" {
				t.Fatalf("expected flash, got %#v", flashes)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/read-again":
			if flashes := store.Flashes("notice"); len(flashes) != 0 {
				t.Fatalf("expected flash to be consumed, got %#v", flashes)
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}))

	setReq := httptest.NewRequest(http.MethodGet, "/set", nil)
	setRes := httptest.NewRecorder()
	handler.ServeHTTP(setRes, setReq)
	if setRes.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", setRes.Code)
	}
	cookie := setRes.Result().Cookies()[0]

	readReq := httptest.NewRequest(http.MethodGet, "/read", nil)
	readReq.AddCookie(cookie)
	readRes := httptest.NewRecorder()
	handler.ServeHTTP(readRes, readReq)
	if readRes.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", readRes.Code)
	}
	cookie = readRes.Result().Cookies()[0]

	readAgainReq := httptest.NewRequest(http.MethodGet, "/read-again", nil)
	readAgainReq.AddCookie(cookie)
	readAgainRes := httptest.NewRecorder()
	handler.ServeHTTP(readAgainRes, readAgainReq)
	if readAgainRes.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", readAgainRes.Code)
	}
}

func TestProtectRejectsMissingOrInvalidToken(t *testing.T) {
	manager := MustNew("csrf-test-secret-value", Options{})
	handler := manager.Middleware(manager.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, Token(r))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})))

	getReq := httptest.NewRequest(http.MethodGet, "/form", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", getRes.Code)
	}
	token := strings.TrimSpace(getRes.Body.String())
	if token == "" {
		t.Fatal("expected csrf token")
	}
	cookie := getRes.Result().Cookies()[0]

	missingReq := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader("name=Ada"))
	missingReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	missingReq.AddCookie(cookie)
	missingRes := httptest.NewRecorder()
	handler.ServeHTTP(missingRes, missingReq)
	if missingRes.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", missingRes.Code)
	}

	validReq := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader("name=Ada&csrf_token="+token))
	validReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	validReq.AddCookie(cookie)
	validRes := httptest.NewRecorder()
	handler.ServeHTTP(validRes, validReq)
	if validRes.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", validRes.Code)
	}

	jsonReq := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader("name=Ada"))
	jsonReq.Header.Set("Accept", "application/json")
	jsonReq.Header.Set("X-CSRF-Token", token)
	jsonReq.AddCookie(cookie)
	jsonRes := httptest.NewRecorder()
	handler.ServeHTTP(jsonRes, jsonReq)
	if jsonRes.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for json csrf header, got %d", jsonRes.Code)
	}

	jsonMissingReq := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader(`{"name":"Ada"}`))
	jsonMissingReq.Header.Set("Accept", "application/json")
	jsonMissingReq.Header.Set("Content-Type", "application/json")
	jsonMissingReq.AddCookie(cookie)
	jsonMissingRes := httptest.NewRecorder()
	handler.ServeHTTP(jsonMissingRes, jsonMissingReq)
	if jsonMissingRes.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for missing json csrf header, got %d", jsonMissingRes.Code)
	}
	if got := jsonMissingRes.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected json csrf failure, got content-type %q", got)
	}
}

// TestProtectAcceptsMultipartFormToken proves the CSRF guard reads the
// csrf_token carried in a multipart/form-data body (e.g. the studio
// workbench's fetch() with a FormData payload and no X-CSRF-Token header).
func TestProtectAcceptsMultipartFormToken(t *testing.T) {
	manager := MustNew("csrf-test-secret-value", Options{})
	handler := manager.Middleware(manager.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, Token(r))
			return
		}
		// Read a downstream field too, to confirm the cached multipart form
		// is still usable by the handler after the middleware parsed it.
		_ = r.FormValue("name")
		w.WriteHeader(http.StatusNoContent)
	})))

	getReq := httptest.NewRequest(http.MethodGet, "/form", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", getRes.Code)
	}
	token := strings.TrimSpace(getRes.Body.String())
	if token == "" {
		t.Fatal("expected csrf token")
	}
	cookie := getRes.Result().Cookies()[0]

	// Multipart POST carrying the valid token in the body and NO header.
	multipartBody := func(withToken bool) (*bytes.Buffer, string) {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		if err := writer.WriteField("name", "Ada"); err != nil {
			t.Fatalf("write name field: %v", err)
		}
		if withToken {
			if err := writer.WriteField(defaultCSRFField, token); err != nil {
				t.Fatalf("write token field: %v", err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close multipart writer: %v", err)
		}
		return &buf, writer.FormDataContentType()
	}

	body, contentType := multipartBody(true)
	validReq := httptest.NewRequest(http.MethodPost, "/form", body)
	validReq.Header.Set("Content-Type", contentType)
	validReq.AddCookie(cookie)
	validRes := httptest.NewRecorder()
	handler.ServeHTTP(validRes, validReq)
	if validRes.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for multipart csrf token, got %d", validRes.Code)
	}

	// Multipart POST WITHOUT the token and no header must still be rejected.
	missingBody, missingContentType := multipartBody(false)
	missingReq := httptest.NewRequest(http.MethodPost, "/form", missingBody)
	missingReq.Header.Set("Content-Type", missingContentType)
	missingReq.AddCookie(cookie)
	missingRes := httptest.NewRecorder()
	handler.ServeHTTP(missingRes, missingReq)
	if missingRes.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for multipart without csrf token, got %d", missingRes.Code)
	}
}

func TestRandomTokenPanicsWhenCryptoRandFails(t *testing.T) {
	original := rand.Reader
	rand.Reader = failingRandReader{}
	t.Cleanup(func() {
		rand.Reader = original
	})

	defer func() {
		if recover() == nil {
			t.Fatal("expected randomToken to panic when crypto/rand fails")
		}
	}()
	_ = randomToken(32)
}

func TestSessionRejectsTamperedCookie(t *testing.T) {
	manager := MustNew("session-tamper-secret-value", Options{})
	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store := Current(r)
		switch r.URL.Path {
		case "/set":
			store.Set("team", "platform")
		case "/read":
			if got := store.String("team"); got != "" {
				t.Fatalf("tampered cookie loaded session value %q", got)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	setRes := httptest.NewRecorder()
	handler.ServeHTTP(setRes, httptest.NewRequest(http.MethodGet, "/set", nil))
	cookie := setRes.Result().Cookies()[0]
	cookie.Value = tamperCookieValue(cookie.Value)

	readReq := httptest.NewRequest(http.MethodGet, "/read", nil)
	readReq.AddCookie(cookie)
	readRes := httptest.NewRecorder()
	handler.ServeHTTP(readRes, readReq)
	if readRes.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", readRes.Code)
	}
}

func TestEncryptedSessionHidesPayload(t *testing.T) {
	manager := MustNew("session-encrypt-secret-value", Options{Encrypt: true})
	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store := Current(r)
		store.Set("team", "platform")
		w.WriteHeader(http.StatusNoContent)
	}))

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/set", nil))
	cookie := res.Result().Cookies()[0]
	if strings.Contains(cookie.Value, "platform") || strings.Contains(cookie.Value, "team") {
		t.Fatalf("encrypted cookie exposed plaintext payload: %q", cookie.Value)
	}
	if _, err := manager.decode(cookie.Value); err != nil {
		t.Fatalf("decode encrypted session: %v", err)
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 3 || parts[0] != "v2" {
		t.Fatalf("expected encrypted v2 cookie, got %q", cookie.Value)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode encrypted body: %v", err)
	}
	if strings.Contains(string(raw), "platform") || strings.Contains(string(raw), "team") {
		t.Fatalf("encrypted payload body exposed plaintext: %q", string(raw))
	}
}

func TestPreviousSecretReadsAndRefreshesLegacyCookie(t *testing.T) {
	oldManager := MustNew("session-old-secret-value", Options{})
	newManager := MustNew("session-new-secret-value", Options{
		Encrypt:         true,
		PreviousSecrets: []string{"session-old-secret-value"},
	})

	oldHandler := oldManager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Current(r).Set("team", "platform")
		w.WriteHeader(http.StatusNoContent)
	}))
	oldRes := httptest.NewRecorder()
	oldHandler.ServeHTTP(oldRes, httptest.NewRequest(http.MethodGet, "/set", nil))
	oldCookie := oldRes.Result().Cookies()[0]
	if strings.HasPrefix(oldCookie.Value, "v2.") {
		t.Fatal("expected legacy signed cookie from old manager")
	}

	newHandler := newManager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := Current(r).String("team"); got != "platform" {
			t.Fatalf("expected rotated session value, got %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	readReq := httptest.NewRequest(http.MethodGet, "/read", nil)
	readReq.AddCookie(oldCookie)
	readRes := httptest.NewRecorder()
	newHandler.ServeHTTP(readRes, readReq)
	if readRes.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", readRes.Code)
	}
	rotated := readRes.Result().Cookies()[0]
	if !strings.HasPrefix(rotated.Value, "v2.") {
		t.Fatalf("expected refreshed encrypted cookie, got %q", rotated.Value)
	}
	if _, err := newManager.decode(rotated.Value); err != nil {
		t.Fatalf("new manager should decode rotated cookie: %v", err)
	}
	if _, err := oldManager.decode(rotated.Value); err == nil {
		t.Fatal("old manager decoded cookie after rotation to the new secret")
	}
}

func FuzzDecodeSessionCookie(f *testing.F) {
	manager := MustNew("session-fuzz-secret-value", Options{Encrypt: true})
	store := &Store{values: map[string]any{"team": "platform"}}
	encoded, err := manager.encode(store)
	if err != nil {
		f.Fatalf("seed encode: %v", err)
	}
	f.Add("")
	f.Add("not.a.cookie")
	f.Add(encoded)
	f.Add(tamperCookieValue(encoded))
	f.Fuzz(func(t *testing.T, value string) {
		_, _ = manager.decode(value)
	})
}

func tamperCookieValue(value string) string {
	if value == "" {
		return "x"
	}
	if value[len(value)-1] == 'A' {
		return value[:len(value)-1] + "B"
	}
	return value[:len(value)-1] + "A"
}
