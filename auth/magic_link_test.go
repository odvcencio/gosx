package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/session"
)

func TestMagicLinkCallbackSignsIn(t *testing.T) {
	sessions := session.MustNew("magic-link-test-secret", session.Options{})
	authn := New(sessions, Options{LoginPath: "/login"})

	var delivered MagicLinkDelivery
	magic := authn.MagicLinks(MagicLinkOptions{
		BaseURL:     "https://app.example",
		SuccessPath: "/welcome",
		Sender: MagicLinkSenderFunc(func(ctx context.Context, delivery MagicLinkDelivery) error {
			delivered = delivery
			return nil
		}),
	})

	requestHandler := sessions.Middleware(magic.RequestHandler())
	callbackHandler := sessions.Middleware(authn.Middleware(magic.CallbackHandler()))
	protected := sessions.Middleware(authn.Middleware(authn.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := Current(r)
		if !ok || user.Email != "ada@example.com" {
			t.Fatalf("expected signed-in magic-link user, got %#v ok=%v", user, ok)
		}
		w.WriteHeader(http.StatusOK)
	}))))

	body := bytes.NewBufferString(`{"email":"ada@example.com","next":"/draft//room/../admin?tab=1"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/request", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res := httptest.NewRecorder()
	requestHandler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if delivered.Token == "" || delivered.URL == "" {
		t.Fatalf("expected delivery payload, got %#v", delivered)
	}

	callbackReq := httptest.NewRequest(http.MethodGet, "/auth/magic-link?token="+delivered.Token, nil)
	callbackRes := httptest.NewRecorder()
	callbackHandler.ServeHTTP(callbackRes, callbackReq)
	if callbackRes.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", callbackRes.Code)
	}
	if location := callbackRes.Header().Get("Location"); location != "/draft/admin?tab=1" {
		t.Fatalf("expected exact canonical redirect, got %q", location)
	}

	var sessionCookie *http.Cookie
	for _, cookie := range callbackRes.Result().Cookies() {
		if cookie.Name != "" {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected auth session cookie after callback")
	}

	protectedReq := httptest.NewRequest(http.MethodGet, "/settings", nil)
	protectedReq.AddCookie(sessionCookie)
	protectedRes := httptest.NewRecorder()
	protected.ServeHTTP(protectedRes, protectedReq)
	if protectedRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", protectedRes.Code)
	}
}

func TestMagicLinkCanonicalizesConfiguredTargetsAndRejectsUnsafeNext(t *testing.T) {
	sessions := session.MustNew("magic-link-return-path-secret", session.Options{})
	authn := New(sessions, Options{})
	m := authn.MagicLinks(MagicLinkOptions{
		BaseURL:     "https://app.example",
		SuccessPath: "/success//flow/../done",
		FailurePath: "//evil.example/failure",
	})
	if m.successPath != "/success/done" {
		t.Fatalf("successPath = %q, want canonical configured path", m.successPath)
	}
	if m.failurePath != "/" {
		t.Fatalf("failurePath = %q, want root fallback", m.failurePath)
	}

	var delivery MagicLinkDelivery
	issue := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		delivery, err = m.Issue(r, "ada@example.com", "/draft%0aLocation:%20/evil")
		if err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	issue.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/issue", nil))
	if delivery.Next != "/" {
		t.Fatalf("unsafe Next = %q, want root fallback", delivery.Next)
	}

	var protocolDelivery MagicLinkDelivery
	protocolIssue := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		protocolDelivery, err = m.Issue(r, "ada@example.com", "//evil.example/steal")
		if err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	protocolIssue.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/issue", nil))
	if protocolDelivery.Next != "/" {
		t.Fatalf("protocol-relative Next = %q, want root fallback", protocolDelivery.Next)
	}

	callback := sessions.Middleware(m.CallbackHandler())
	res := httptest.NewRecorder()
	callback.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/auth/magic-link?token="+delivery.Token, nil))
	if location := res.Header().Get("Location"); location != "/" {
		t.Fatalf("unsafe Next redirect = %q, want exact root fallback", location)
	}
}

func TestMagicLinkUnsafeNextUsesRootFallback(t *testing.T) {
	sessions := session.MustNew("magic-link-sanitize-secret", session.Options{})
	authn := New(sessions, Options{})
	magic := authn.MagicLinks(MagicLinkOptions{
		BaseURL:     "https://app.example",
		SuccessPath: "/safe",
	})

	var delivery MagicLinkDelivery
	issue := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		delivery, err = magic.Issue(r, "ada@example.com", "https://evil.example")
		if err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/issue", nil)
	res := httptest.NewRecorder()
	issue.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}
	if delivery.Next != "/" {
		t.Fatalf("expected unsafe next to use root fallback, got %q", delivery.Next)
	}

	callback := sessions.Middleware(magic.CallbackHandler())
	callbackReq := httptest.NewRequest(http.MethodGet, "/auth/magic-link?token="+delivery.Token, nil)
	callbackRes := httptest.NewRecorder()
	callback.ServeHTTP(callbackRes, callbackReq)
	if callbackRes.Header().Get("Location") != "/" {
		t.Fatalf("expected invalid next redirect to root, got %q", callbackRes.Header().Get("Location"))
	}
}

func TestMagicLinkRejectsExpiredToken(t *testing.T) {
	now := time.Date(2026, 3, 26, 14, 0, 0, 0, time.UTC)
	sessions := session.MustNew("magic-link-expired-secret", session.Options{})
	authn := New(sessions, Options{})
	magic := authn.MagicLinks(MagicLinkOptions{
		BaseURL: "https://app.example",
		TTL:     time.Minute,
		Now:     func() time.Time { return now },
		Store:   NewMemoryMagicLinkStore(),
	})

	var token string
	issue := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivery, err := magic.Issue(r, "ada@example.com", "/")
		if err != nil {
			t.Fatal(err)
		}
		token = delivery.Token
		w.WriteHeader(http.StatusNoContent)
	}))
	issue.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/issue", nil))

	magic.now = func() time.Time { return now.Add(2 * time.Minute) }
	consume := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, err := magic.Consume(r, token)
		if err != ErrMagicLinkExpired {
			t.Fatalf("expected ErrMagicLinkExpired, got %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	res := httptest.NewRecorder()
	consume.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/consume", nil))
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}
}

func TestMagicLinkRequestHandlerRejectsInvalidJSON(t *testing.T) {
	sessions := session.MustNew("magic-link-json-secret", session.Options{})
	authn := New(sessions, Options{})
	magic := authn.MagicLinks(MagicLinkOptions{})

	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/request", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res := httptest.NewRecorder()
	sessions.Middleware(magic.RequestHandler()).ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected json error payload: %v", err)
	}
	if payload["error"] == nil {
		t.Fatalf("expected error payload, got %#v", payload)
	}
}
