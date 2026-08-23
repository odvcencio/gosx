package action

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"m31labs.dev/gosx/session"
)

func newManagedTestSession(t *testing.T) (*session.Manager, string, string) {
	t.Helper()
	manager, err := session.New("managed-action-test-secret", session.Options{
		CookieName:    "managed_action_test",
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var token string
	first := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		token = session.Token(req)
	})).ServeHTTP(recorder, first)
	cookie := recorder.Header().Get("Set-Cookie")
	if semi := strings.IndexByte(cookie, ';'); semi >= 0 {
		cookie = cookie[:semi]
	}
	if token == "" || cookie == "" {
		t.Fatalf("session bootstrap token=%q cookie=%q", token, cookie)
	}
	return manager, token, cookie
}

func managedRequest(t *testing.T, manager *session.Manager, router *Router, token, cookie string, method, target, contentType string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("X-CSRF-Token", token)
	req.Header.Set("Cookie", cookie)
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	return recorder
}

func decodeManaged(t *testing.T, recorder *httptest.ResponseRecorder) ManagedOutcome {
	t.Helper()
	var outcome ManagedOutcome
	if err := json.Unmarshal(recorder.Body.Bytes(), &outcome); err != nil {
		t.Fatalf("decode managed response: %v; body=%s", err, recorder.Body.String())
	}
	return outcome
}

func TestRegisterManagedPOSTResolvesDefaultsAndRejectsInvalidConfig(t *testing.T) {
	router := NewRouter()
	if err := router.RegisterManagedPOST("defaults", Config{}, func(*Context) (Result, error) {
		return Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.RegisterManagedPOST("defaults", Config{}, func(*Context) (Result, error) {
		return Result{OK: true}, nil
	}); err == nil {
		t.Fatal("duplicate route registration succeeded")
	}
	if err := router.RegisterManagedPOST("partial", Config{CSRF: CSRFConfig{HeaderName: "X-Test"}}, func(*Context) (Result, error) {
		return Result{OK: true}, nil
	}); err == nil {
		t.Fatal("partial CSRF config succeeded")
	}
	if err := router.RegisterManagedPOST("files", Config{Limits: Limits{MaxFiles: 1}}, func(*Context) (Result, error) {
		return Result{OK: true}, nil
	}); err == nil {
		t.Fatal("file-enabled config without MaxFileBytes succeeded")
	}
	if err := router.RegisterManagedPOST("values", Config{ResultValues: ResultValuesConfig{AllowedNames: []string{"x", "x"}, MaxSerializedBytes: 32}}, func(*Context) (Result, error) {
		return Result{OK: true}, nil
	}); err == nil {
		t.Fatal("duplicate result value name succeeded")
	}
}

func TestManagedJSONDispatchDecodesPayloadAndSeparatesQuery(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	if err := router.RegisterManagedPOST("json", Config{}, func(ctx *Context) (Result, error) {
		payload, ok := ctx.Payload.(map[string]any)
		if !ok || payload["name"] != "Ada" {
			t.Fatalf("payload = %#v", ctx.Payload)
		}
		if got := ctx.Request.URL.Query().Get("csrf_token"); got != "query-only" {
			t.Fatalf("query token = %q", got)
		}
		if ctx.Form.Value("name") != "" || ctx.Form.Values("name") != nil {
			t.Fatal("JSON action unexpectedly exposed form values")
		}
		return Result{OK: true, Message: "saved", Data: json.RawMessage(`{"ok":true}`)}, nil
	}); err != nil {
		t.Fatal(err)
	}
	recorder := managedRequest(t, manager, router, token, cookie, http.MethodPost, "/gosx/action/json?csrf_token=query-only", "application/json", strings.NewReader(`{"name":"Ada"}`))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	outcome := decodeManaged(t, recorder)
	if !outcome.OK || outcome.Code != "ok" || outcome.Message != "saved" || string(outcome.Data) != `{"ok":true}` {
		t.Fatalf("outcome = %#v", outcome)
	}
}

func TestManagedJSONRequiresCSRFHeaderAndRejectsTrailingValue(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	var calls atomic.Int32
	if err := router.RegisterManagedPOST("json", Config{}, func(*Context) (Result, error) {
		calls.Add(1)
		return Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/json", strings.NewReader(`{"ok":true} {"extra":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden || calls.Load() != 0 {
		t.Fatalf("missing header status=%d calls=%d body=%s", recorder.Code, calls.Load(), recorder.Body.String())
	}
	recorder = managedRequest(t, manager, router, token, cookie, http.MethodPost, "/gosx/action/json", "application/json", strings.NewReader(`{"ok":true} {"extra":true}`))
	if recorder.Code != http.StatusBadRequest || calls.Load() != 0 {
		t.Fatalf("trailing value status=%d calls=%d body=%s", recorder.Code, calls.Load(), recorder.Body.String())
	}
}

func TestManagedURLFormPreservesDuplicatesAndBodyCSRF(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	var got []string
	if err := router.RegisterManagedPOST("form", Config{}, func(ctx *Context) (Result, error) {
		got = ctx.Form.Values("tag")
		if ctx.Form.Value("empty") != "" {
			t.Fatal("empty value was not preserved as empty")
		}
		if ctx.Request.Form.Get("query-only") != "" {
			t.Fatal("query values leaked into Request.Form")
		}
		return Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	// Bootstrap a second request without a header so the body fallback is
	// exercised against the same signed session token.
	body := "tag=one&tag=two&empty=&csrf_token=" + urlQueryEscape(token)
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/form?query-only=ignored", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookie)
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Join(got, ",") != "one,two" {
		t.Fatalf("duplicate values=%v", got)
	}
}

func TestManagedMultipartUploadStreamsAndCleansSpill(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	var uploaded []byte
	var filename string
	if err := router.RegisterManagedPOST("upload", Config{
		Limits: Limits{MaxFiles: 1, MaxFileBytes: 64, MaxMemoryFileBytes: 4},
	}, func(ctx *Context) (Result, error) {
		filename = ctx.Form.Files("avatar")[0].ClientFilename
		file, err := ctx.Form.File("avatar")
		if err != nil {
			return Result{}, err
		}
		reader, err := file.Open()
		if err != nil {
			return Result{}, err
		}
		defer reader.Close()
		uploaded, err = io.ReadAll(reader)
		if err != nil {
			return Result{}, err
		}
		return Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("csrf_token", token); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("avatar", "../a\\b-very-long.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Cookie", cookie)
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if string(uploaded) != "0123456789" || filename != ".._a_b-very-long.bin" {
		t.Fatalf("uploaded=%q filename=%q", uploaded, filename)
	}
}

func TestManagedMultipartRejectsFilesWhenDisabled(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	if err := router.RegisterManagedPOST("upload", Config{}, func(*Context) (Result, error) {
		t.Fatal("handler ran for disabled file")
		return Result{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("avatar", "avatar.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("x"))
	if err := writer.WriteField("csrf_token", token); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Cookie", cookie)
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestMultipartDispositionRejectsExtendedFilenameParameters(t *testing.T) {
	for _, disposition := range []string{
		`form-data; name="avatar"; filename*=UTF-8''avatar.png`,
		`form-data; name="avatar"; filename*0="avatar.png"`,
		`form-data; name*=UTF-8''avatar`,
	} {
		if _, _, _, err := multipartDisposition(textproto.MIMEHeader{"Content-Disposition": []string{disposition}}); err == nil {
			t.Fatalf("accepted extended filename parameter %q", disposition)
		}
	}
}

func TestManagedHeaderCSRFIsAuthoritativeWithoutReadingBody(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	if err := router.RegisterManagedPOST("csrf", Config{}, func(*Context) (Result, error) {
		t.Fatal("handler ran for invalid CSRF")
		return Result{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int32
	body := &countingReadCloser{Reader: strings.NewReader("not json"), reads: &reads}
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/csrf", body)
	req.Body = body
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", "wrong")
	req.Header.Set("Cookie", cookie)
	recorder := httptest.NewRecorder()
	manager.Middleware(manager.Protect(router)).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden || reads.Load() != 0 {
		t.Fatalf("status=%d reads=%d body=%s", recorder.Code, reads.Load(), recorder.Body.String())
	}
	_ = token
}

func TestSessionProtectDoesNotTrustManagedActionPathPrefix(t *testing.T) {
	manager, _, cookie := newManagedTestSession(t)
	raw := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/not-registered", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookie)
	recorder := httptest.NewRecorder()
	manager.Middleware(manager.Protect(raw)).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("prefix-matching app handler bypassed CSRF: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	legacy := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	req = httptest.NewRequest(http.MethodPost, "/__actions/not-registered", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookie)
	recorder = httptest.NewRecorder()
	manager.Middleware(manager.Protect(legacy)).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("legacy prefix-matching app handler bypassed CSRF: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

type managedActionTestWrapper struct{ http.Handler }

func (w managedActionTestWrapper) Unwrap() http.Handler { return w.Handler }

func TestSessionProtectFindsManagedCapabilityThroughWrapper(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	if err := router.RegisterManagedPOST("wrapped", Config{}, func(ctx *Context) (Result, error) {
		if got := ctx.Form.Value("csrf_token"); got != token {
			t.Fatalf("body CSRF token = %q", got)
		}
		return Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/wrapped", strings.NewReader("csrf_token="+urlQueryEscape(token)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookie)
	recorder := httptest.NewRecorder()
	manager.Middleware(manager.Protect(managedActionTestWrapper{Handler: router})).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("wrapped managed action status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestManagedResultValuesAndRedirectValidation(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	if err := router.RegisterManagedPOST("result", Config{ResultValues: ResultValuesConfig{AllowedNames: []string{"flash"}, MaxSerializedBytes: 64}}, func(*Context) (Result, error) {
		return Result{OK: true, Values: map[string]string{"flash": "saved"}, Redirect: "/done"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	recorder := managedRequest(t, manager, router, token, cookie, http.MethodPost, "/gosx/action/result", "application/x-www-form-urlencoded", strings.NewReader("csrf_token="+urlQueryEscape(token)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	outcome := decodeManaged(t, recorder)
	if outcome.Redirect != "/done" || outcome.RedirectStatus != http.StatusSeeOther || outcome.Values["flash"] != "saved" {
		t.Fatalf("outcome=%#v", outcome)
	}

	badRouter := NewRouter()
	if err := badRouter.RegisterManagedPOST("bad", Config{}, func(*Context) (Result, error) {
		return Result{OK: true, RedirectStatus: http.StatusSeeOther}, nil
	}); err != nil {
		t.Fatal(err)
	}
	recorder = managedRequest(t, manager, badRouter, token, cookie, http.MethodPost, "/gosx/action/bad", "application/x-www-form-urlencoded", strings.NewReader("csrf_token="+urlQueryEscape(token)))
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "internal_error") {
		t.Fatalf("invalid redirect status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

type countingReadCloser struct {
	io.Reader
	reads *atomic.Int32
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	r.reads.Add(1)
	return r.Reader.Read(p)
}

func (r *countingReadCloser) Close() error { return nil }

func urlQueryEscape(value string) string {
	return url.QueryEscape(value)
}
