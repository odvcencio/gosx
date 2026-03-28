package session

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
}
