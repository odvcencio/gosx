package action

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"sync"
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

func TestActionNameGrammarAndCanonicalPath(t *testing.T) {
	valid64 := "a" + strings.Repeat("x", 63)
	cases := []struct {
		name string
		ok   bool
	}{
		{name: "a", ok: true},
		{name: "Save_2-now", ok: true},
		{name: valid64, ok: true},
		{name: "", ok: false},
		{name: "1starts-with-digit", ok: false},
		{name: "-starts-with-dash", ok: false},
		{name: valid64 + "x", ok: false},
		{name: "naïve", ok: false},
		{name: "has space", ok: false},
		{name: "has/slash", ok: false},
		{name: "has?query", ok: false},
		{name: "has#fragment", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateActionName(tc.name)
			if (err == nil) != tc.ok {
				t.Fatalf("ValidateActionName(%q) error=%v, valid=%v", tc.name, err, tc.ok)
			}
			path := ActionPath(tc.name)
			if tc.ok && path != "/gosx/action/"+tc.name {
				t.Fatalf("ActionPath(%q)=%q", tc.name, path)
			}
			if !tc.ok && path != "" {
				t.Fatalf("ActionPath(%q)=%q for invalid name", tc.name, path)
			}
		})
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

func TestManagedMultipartNestedDeclaredTypeIsOpaque(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	var gotMedia string
	var gotBody []byte
	if err := router.RegisterManagedPOST("nested", Config{Limits: Limits{MaxFiles: 1, MaxFileBytes: 64}}, func(ctx *Context) (Result, error) {
		upload, err := ctx.Form.File("payload")
		if err != nil {
			return Result{}, err
		}
		gotMedia = upload.DeclaredMediaType
		reader, err := upload.Open()
		if err != nil {
			return Result{}, err
		}
		defer reader.Close()
		gotBody, err = io.ReadAll(reader)
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
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", `form-data; name="payload"; filename="opaque.bin"`)
	partHeader.Set("Content-Type", `multipart/mixed; boundary=inner`)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("--inner\r\nopaque\r\n--inner--\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	recorder := managedRequest(t, manager, router, token, cookie, http.MethodPost, "/gosx/action/nested", writer.FormDataContentType(), &body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if gotMedia != "multipart/mixed" || string(gotBody) != "--inner\r\nopaque\r\n--inner--\r\n" {
		t.Fatalf("opaque upload media=%q body=%q", gotMedia, gotBody)
	}
}

func TestManagedCustomCSRFHeaderAndBodyField(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	if err := router.RegisterManagedPOST("custom-csrf", Config{CSRF: CSRFConfig{
		HeaderName:    "X-Custom-CSRF",
		BodyFieldName: "_csrf",
	}}, func(ctx *Context) (Result, error) {
		if ctx.Payload.(map[string]any)["name"] != "Ada" {
			t.Fatal("payload was not decoded")
		}
		return Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/custom-csrf", strings.NewReader(`{"name":"Ada"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Custom-CSRF", token)
	req.Header.Set("Cookie", cookie)
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("custom header status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestManagedRedirectIsValidatedBeforeSessionCommit(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	if err := router.RegisterManagedPOST("redirect", Config{}, func(*Context) (Result, error) {
		return Result{OK: true, Redirect: "//outside.example/path"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	recorder := managedRequest(t, manager, router, token, cookie, http.MethodPost, "/gosx/action/redirect", "application/x-www-form-urlencoded", strings.NewReader(""))
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "internal_error") {
		t.Fatalf("hostile redirect status=%d body=%s", recorder.Code, recorder.Body.String())
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

func TestMultipartMetadataBudgetStopsBeforeMIMEHeaderMaterialization(t *testing.T) {
	boundary := "gosx-phasec-budget"
	maxMetadata := int64(64 << 10)
	raw := "--" + boundary + "\r\nX-Long: " + strings.Repeat("x", 10<<20) + "\r\n\r\n--" + boundary + "--\r\n"

	// Exercise the guard at its raw-byte seam as well as through the action
	// dispatcher. The source must not be consumed through Go's independent
	// 10 MiB textproto ceiling before GoSX reports its 64 KiB policy error.
	directSource := &byteCountingReadCloser{Reader: strings.NewReader(raw)}
	scanner := newMultipartMetadataBudgetReader(directSource, boundary, maxMetadata)
	buf := make([]byte, 32<<10)
	var scanErr error
	for scanErr == nil {
		_, scanErr = scanner.Read(buf)
	}
	if !errors.Is(scanErr, errMultipartMetadataLimit) {
		t.Fatalf("scanner error=%v, want metadata limit", scanErr)
	}
	if scanner.metadata != maxMetadata || directSource.bytes.Load() >= int64(10<<20) {
		t.Fatalf("scanner metadata=%d source bytes=%d, want exact allowance rejection before 10 MiB", scanner.metadata, directSource.bytes.Load())
	}

	manager, token, cookie := newManagedTestSession(t)
	var actionCalls atomic.Int32
	router := NewRouter()
	if err := router.RegisterManagedPOST("metadata-budget", Config{Limits: Limits{
		MaxRequestBodyBytes:       int64(12 << 20),
		MaxMultipartMetadataBytes: maxMetadata,
	}}, func(*Context) (Result, error) {
		actionCalls.Add(1)
		return Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	source := &byteCountingReadCloser{Reader: strings.NewReader(raw)}
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/metadata-budget", source)
	req.Body = source
	req.ContentLength = int64(len(raw))
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	req.Header.Set("X-CSRF-Token", token)
	req.Header.Set("Cookie", cookie)
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusRequestEntityTooLarge || actionCalls.Load() != 0 {
		t.Fatalf("status=%d actionCalls=%d body=%s", recorder.Code, actionCalls.Load(), recorder.Body.String())
	}
	if outcome := decodeManaged(t, recorder); outcome.Code != "request_too_large" || outcome.Message != "request is too large" {
		t.Fatalf("outcome=%#v, want typed generic metadata response", outcome)
	}
	if source.bytes.Load() >= int64(10<<20) {
		t.Fatalf("dispatcher consumed %d bytes before metadata rejection", source.bytes.Load())
	}
}

func TestMultipartMetadataScannerPreservesPreambleQuotedHeadersAndEpilogue(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	boundary := "gosx-phasec-normal"
	raw := "preamble text\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Disposition: form-data; name=\"csrf_token\"\r\n" +
		"\r\n" + token + "\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Disposition: form-data; name=\"note\"\r\n" +
		"X-Repeat: one\r\nX-Repeat: two\r\n" +
		"\r\nhello\r\n" +
		"--" + boundary + "--\r\n" +
		"epilogue text"
	router := NewRouter()
	if err := router.RegisterManagedPOST("metadata-normal", Config{Limits: Limits{MaxMultipartMetadataBytes: 4096}}, func(ctx *Context) (Result, error) {
		if ctx.Form.Value("note") != "hello" {
			return Result{}, errors.New("multipart note did not survive raw scanner")
		}
		return Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/metadata-normal", strings.NewReader(raw))
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	req.Header.Set("Cookie", cookie)
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if outcome := decodeManaged(t, recorder); !outcome.OK || outcome.Code != "ok" {
		t.Fatalf("outcome=%#v", outcome)
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
	req = httptest.NewRequest(http.MethodPost, "/gosx/action/not-registered", strings.NewReader(""))
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

func (w managedActionTestWrapper) PreservesManagedActionCapability() bool { return true }

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

type byteCountingReadCloser struct {
	io.Reader
	bytes atomic.Int64
}

func (r *byteCountingReadCloser) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 {
		r.bytes.Add(int64(n))
	}
	return n, err
}

func (r *byteCountingReadCloser) Close() error { return nil }

func urlQueryEscape(value string) string {
	return url.QueryEscape(value)
}

func sessionCookieForResponse(recorder *httptest.ResponseRecorder, fallback string) string {
	if recorder != nil {
		if cookies := recorder.Result().Cookies(); len(cookies) > 0 {
			return cookies[len(cookies)-1].String()
		}
	}
	return fallback
}

func readManagedFlashes(t *testing.T, manager *session.Manager, cookie string) map[string][]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/done", nil)
	req.Header.Set("Cookie", cookie)
	var flashes map[string][]any
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flashes = session.FlashValues(r)
	})).ServeHTTP(httptest.NewRecorder(), req)
	return flashes
}

func TestManagedNativeURLFlashCommitsBeforeRedirect(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	if err := router.RegisterManagedPOST("flash", Config{ResultValues: ResultValuesConfig{
		AllowedNames:       []string{"notice"},
		MaxSerializedBytes: 64,
		NativeFlashValues:  true,
	}}, func(*Context) (Result, error) {
		return Result{OK: true, Values: map[string]string{"notice": "saved"}, Redirect: "/done"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/flash", strings.NewReader("csrf_token="+urlQueryEscape(token)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Cookie", cookie)
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/done" {
		t.Fatalf("native redirect status=%d location=%q body=%s", recorder.Code, recorder.Header().Get("Location"), recorder.Body.String())
	}
	flashes := readManagedFlashes(t, manager, sessionCookieForResponse(recorder, cookie))
	if got := flashes["notice"]; len(got) != 1 || got[0] != "saved" {
		t.Fatalf("native flash=%v", flashes)
	}
}

func TestManagedMultipartRedirectNeverFlashesValues(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	if err := router.RegisterManagedPOST("multipart-flash", Config{
		Limits: Limits{MaxFiles: 1, MaxFileBytes: 32},
		ResultValues: ResultValuesConfig{
			AllowedNames:       []string{"notice"},
			MaxSerializedBytes: 64,
			NativeFlashValues:  true,
		},
	}, func(*Context) (Result, error) {
		return Result{OK: true, Values: map[string]string{"notice": "must-not-flash"}, Redirect: "/done"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("csrf_token", token); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/multipart-flash", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Cookie", cookie)
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/done" {
		t.Fatalf("multipart redirect status=%d location=%q body=%s", recorder.Code, recorder.Header().Get("Location"), recorder.Body.String())
	}
	if flashes := readManagedFlashes(t, manager, sessionCookieForResponse(recorder, cookie)); len(flashes) != 0 {
		t.Fatalf("multipart unexpectedly flashed values: %v", flashes)
	}
}

func TestManagedUploadSentinelsMapToTypedGeneric422(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	for _, tc := range []struct {
		name string
		body func(*testing.T, string) (*http.Request, error)
		want RequestErrorKind
	}{
		{
			name: "missing",
			body: func(t *testing.T, token string) (*http.Request, error) {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				if err := writer.WriteField("csrf_token", token); err != nil {
					return nil, err
				}
				if err := writer.Close(); err != nil {
					return nil, err
				}
				req := httptest.NewRequest(http.MethodPost, "/gosx/action/missing", &body)
				req.Header.Set("Content-Type", writer.FormDataContentType())
				return req, nil
			},
			want: RequestErrorMissingUpload,
		},
		{
			name: "multiple",
			body: func(t *testing.T, token string) (*http.Request, error) {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				if err := writer.WriteField("csrf_token", token); err != nil {
					return nil, err
				}
				for _, value := range []string{"one", "two"} {
					part, err := writer.CreateFormFile("avatar", value+".txt")
					if err != nil {
						return nil, err
					}
					if _, err := part.Write([]byte(value)); err != nil {
						return nil, err
					}
				}
				if err := writer.Close(); err != nil {
					return nil, err
				}
				req := httptest.NewRequest(http.MethodPost, "/gosx/action/multiple", &body)
				req.Header.Set("Content-Type", writer.FormDataContentType())
				return req, nil
			},
			want: RequestErrorMultipleUpload,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouter()
			maxFiles := 1
			if tc.want == RequestErrorMultipleUpload {
				maxFiles = 2
			}
			if err := router.RegisterManagedPOST(tc.name, Config{Limits: Limits{MaxFiles: maxFiles, MaxFileBytes: 64}}, func(ctx *Context) (Result, error) {
				_, err := ctx.Form.File("avatar")
				return Result{}, err
			}); err != nil {
				t.Fatal(err)
			}
			req, err := tc.body(t, token)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Cookie", cookie)
			recorder := httptest.NewRecorder()
			manager.Middleware(router).ServeHTTP(recorder, req)
			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			outcome := decodeManaged(t, recorder)
			if outcome.Code != "validation_error" || outcome.Message != "request could not be processed" {
				t.Fatalf("outcome=%#v, want generic validation response", outcome)
			}
		})
	}
	router := NewRouter()
	if err := router.RegisterManagedPOST("combined-upload-sentinels", Config{}, func(*Context) (Result, error) {
		return Result{}, errors.Join(ErrMissingUpload, ErrMultipleUploads)
	}); err != nil {
		t.Fatal(err)
	}
	recorder := managedRequest(t, manager, router, token, cookie, http.MethodPost, "/gosx/action/combined-upload-sentinels", "application/x-www-form-urlencoded", strings.NewReader("csrf_token="+urlQueryEscape(token)))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("combined sentinel status=%d body=%s, want 500", recorder.Code, recorder.Body.String())
	}
	if outcome := decodeManaged(t, recorder); outcome.Code != "internal_error" || outcome.Message != "internal server error" {
		t.Fatalf("combined sentinel outcome=%#v, want generic 500", outcome)
	}
}

func TestManagedSpillReaderInvalidatesAndRemoveFailureDoesNotRewriteSuccess(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	originalStorage := uploadStorage
	t.Cleanup(func() { uploadStorage = originalStorage })
	var removeCalls int
	storage := originalStorage
	storage.remove = func(name string) error {
		removeCalls++
		return errors.New("injected remove failure")
	}
	uploadStorage = storage
	router := NewRouter()
	var held Upload
	var heldReader io.ReadCloser
	if err := router.RegisterManagedPOST("spill", Config{Limits: Limits{MaxFiles: 1, MaxFileBytes: 64, MaxMemoryFileBytes: 1}}, func(ctx *Context) (Result, error) {
		var err error
		held, err = ctx.Form.File("avatar")
		if err != nil {
			return Result{}, err
		}
		heldReader, err = held.Open()
		if err != nil {
			return Result{}, err
		}
		_, err = io.ReadAll(heldReader)
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
	part, err := writer.CreateFormFile("avatar", "avatar.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("spill-me"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/gosx/action/spill", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Cookie", cookie)
	req.Header.Set("X-CSRF-Token", token)
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"ok":true`) {
		t.Fatalf("spill status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if removeCalls != 1 {
		t.Fatalf("remove calls=%d, want one terminal attempt", removeCalls)
	}
	if _, err := held.Open(); err == nil {
		t.Fatal("upload remained open after action return")
	}
	if _, err := heldReader.Read(make([]byte, 1)); err == nil {
		t.Fatal("reader remained usable after action return")
	}
}

func TestManagedCleanupLogsAggregateExactlyOnce(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	originalStorage := uploadStorage
	t.Cleanup(func() { uploadStorage = originalStorage })
	createErr := errors.New("reader close failed")
	removeErr := errors.New("remove failed")
	storage := defaultUploadStorage
	var closeCalls int
	storage.createTemp = func() (uploadTempFile, error) {
		return &testUploadTemp{name: "/tmp/gosx-managed-cleanup-log"}, nil
	}
	storage.close = func(io.Closer) error {
		closeCalls++
		if closeCalls > 1 {
			return createErr
		}
		return nil
	}
	storage.remove = func(string) error { return removeErr }
	storage.open = func(string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("reader")), nil }
	uploadStorage = storage

	var logs bytes.Buffer
	oldLogWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldLogWriter) })
	router := NewRouter()
	if err := router.RegisterManagedPOST("cleanup-log", Config{Limits: Limits{MaxFiles: 1, MaxFileBytes: 64, MaxMemoryFileBytes: 1}}, func(ctx *Context) (Result, error) {
		upload, err := ctx.Form.File("avatar")
		if err != nil {
			return Result{}, err
		}
		reader, err := upload.Open()
		if err != nil {
			return Result{}, err
		}
		_, _ = io.ReadAll(reader)
		return Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := managedMultipartUploadRequest(t, "/gosx/action/cleanup-log", token, cookie, "cleanup.bin", []byte("spill"))
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if count := strings.Count(logs.String(), "[gosx] managed action cleanup failed:"); count != 1 {
		t.Fatalf("cleanup log count=%d logs=%q", count, logs.String())
	}
	if !strings.Contains(logs.String(), createErr.Error()) || !strings.Contains(logs.String(), removeErr.Error()) {
		t.Fatalf("cleanup log dropped aggregate causes: %q", logs.String())
	}
}

func TestManagedPreActionParseCleanupOwnsEarlierUploadsOnce(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	originalStorage := uploadStorage
	t.Cleanup(func() { uploadStorage = originalStorage })
	removeErr := errors.New("pre-action remove failed")
	storage := defaultUploadStorage
	storage.createTemp = func() (uploadTempFile, error) {
		return &testUploadTemp{name: "/tmp/gosx-managed-pre-action"}, nil
	}
	storage.remove = func(string) error { return removeErr }
	storage.open = func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("unused")), nil
	}
	uploadStorage = storage

	var actionCalls atomic.Int32
	router := NewRouter()
	if err := router.RegisterManagedPOST("pre-action-cleanup", Config{Limits: Limits{
		MaxFiles:           1,
		MaxFileBytes:       64,
		MaxMemoryFileBytes: 1,
	}}, func(*Context) (Result, error) {
		actionCalls.Add(1)
		return Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("csrf_token", token); err != nil {
		t.Fatal(err)
	}
	filePart, err := writer.CreateFormFile("avatar", "avatar.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := filePart.Write([]byte("spill")); err != nil {
		t.Fatal(err)
	}
	malformedHeader := make(textproto.MIMEHeader)
	malformedHeader.Set("Content-Disposition", `form-data; name="broken"; unsupported="x"`)
	fieldPart, err := writer.CreatePart(malformedHeader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fieldPart.Write([]byte("never reaches action")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/gosx/action/pre-action-cleanup", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Cookie", cookie)
	var logs bytes.Buffer
	oldLogWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldLogWriter) })
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest || actionCalls.Load() != 0 {
		t.Fatalf("status=%d actionCalls=%d body=%s", recorder.Code, actionCalls.Load(), recorder.Body.String())
	}
	if count := strings.Count(logs.String(), "[gosx] managed action cleanup failed:"); count != 1 {
		t.Fatalf("cleanup log count=%d logs=%q", count, logs.String())
	}
	if !strings.Contains(logs.String(), removeErr.Error()) {
		t.Fatalf("cleanup log dropped earlier-upload remove cause: %q", logs.String())
	}
}

func TestManagedMemoryUploadReadersAreRevokedAndCloseIsIdempotent(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	var held Upload
	var readers []io.ReadCloser
	if err := router.RegisterManagedPOST("memory-lifecycle", Config{Limits: Limits{MaxFiles: 1, MaxFileBytes: 64, MaxMemoryFileBytes: 64}}, func(ctx *Context) (Result, error) {
		var err error
		held, err = ctx.Form.File("avatar")
		if err != nil {
			return Result{}, err
		}
		for i := 0; i < 2; i++ {
			reader, openErr := held.Open()
			if openErr != nil {
				return Result{}, openErr
			}
			readers = append(readers, reader)
			if _, readErr := io.ReadAll(reader); readErr != nil {
				return Result{}, readErr
			}
		}
		return Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := managedMultipartUploadRequest(t, "/gosx/action/memory-lifecycle", token, cookie, "memory.txt", []byte("memory"))
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("memory lifecycle status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if held.state == nil || held.state.openFn != nil {
		t.Fatal("memory upload retained its backing open closure after cleanup")
	}
	if _, err := held.Open(); err == nil {
		t.Fatal("memory upload reopened after action return")
	}
	for i, reader := range readers {
		if _, err := reader.Read(make([]byte, 1)); err == nil {
			t.Fatalf("memory reader %d remained readable after action return", i)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("memory reader %d first Close: %v", i, err)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("memory reader %d second Close: %v", i, err)
		}
	}
}

func TestManagedTempUploadReadersAreRevokedAndRemoved(t *testing.T) {
	manager, token, cookie := newManagedTestSession(t)
	router := NewRouter()
	var held Upload
	var readers []io.ReadCloser
	var tempPath string
	if err := router.RegisterManagedPOST("temp-lifecycle", Config{Limits: Limits{MaxFiles: 1, MaxFileBytes: 64, MaxMemoryFileBytes: 1}}, func(ctx *Context) (Result, error) {
		var err error
		held, err = ctx.Form.File("avatar")
		if err != nil {
			return Result{}, err
		}
		tempPath = held.state.tempPath
		for i := 0; i < 2; i++ {
			reader, openErr := held.Open()
			if openErr != nil {
				return Result{}, openErr
			}
			readers = append(readers, reader)
			if _, readErr := io.ReadAll(reader); readErr != nil {
				return Result{}, readErr
			}
		}
		return Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := managedMultipartUploadRequest(t, "/gosx/action/temp-lifecycle", token, cookie, "temp.bin", []byte("spill"))
	recorder := httptest.NewRecorder()
	manager.Middleware(router).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("temp lifecycle status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if tempPath == "" {
		t.Fatal("temp lifecycle did not spill to a temp path")
	}
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("temp path still exists after cleanup: %q err=%v", tempPath, err)
	}
	if _, err := held.Open(); err == nil {
		t.Fatal("temp upload reopened after action return")
	}
	for i, reader := range readers {
		if _, err := reader.Read(make([]byte, 1)); err == nil {
			t.Fatalf("temp reader %d remained readable after action return", i)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("temp reader %d first Close: %v", i, err)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("temp reader %d second Close: %v", i, err)
		}
	}
}

func TestUploadOpenInvalidateRace(t *testing.T) {
	state := &uploadState{
		valid:    true,
		openKind: RequestErrorUploadOpen,
		openFn: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("concurrent")), nil
		},
	}
	const workers = 64
	start := make(chan struct{})
	var wg sync.WaitGroup
	var openFailures atomic.Int32
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			reader, err := state.open()
			if err != nil {
				openFailures.Add(1)
				return
			}
			_, _ = io.ReadAll(reader)
			_ = reader.Close()
		}()
	}
	var invalidateWG sync.WaitGroup
	invalidateWG.Add(1)
	go func() {
		defer invalidateWG.Done()
		<-start
		_ = state.invalidate()
	}()
	close(start)
	wg.Wait()
	invalidateWG.Wait()
	if err := state.cleanup(); err != nil {
		t.Fatalf("terminal cleanup returned error: %v", err)
	}
	if state.openFn != nil || state.valid {
		t.Fatal("upload state remained valid after concurrent invalidation")
	}
	if _, err := state.open(); err == nil {
		t.Fatal("upload state reopened after concurrent invalidation")
	}
}

func managedMultipartUploadRequest(t *testing.T, path, token, cookie, filename string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("csrf_token", token); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("avatar", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Cookie", cookie)
	return req
}

func TestCheckedAddExactFirstOverAndOverflowBoundaries(t *testing.T) {
	const maxInt64 = int64(1<<63 - 1)
	cases := []struct {
		name           string
		current, delta int64
		limit          int64
		want           int64
		wantOK         bool
	}{
		{name: "zero exact", current: 0, delta: 0, limit: 0, want: 0, wantOK: true},
		{name: "first exact", current: 0, delta: 1, limit: 1, want: 1, wantOK: true},
		{name: "current exact", current: 1, delta: 0, limit: 1, want: 1, wantOK: true},
		{name: "first over", current: 0, delta: 2, limit: 1, want: 0, wantOK: false},
		{name: "current over", current: 2, delta: 0, limit: 1, want: 2, wantOK: false},
		{name: "remaining over", current: 1, delta: 1, limit: 1, want: 1, wantOK: false},
		{name: "max exact", current: 0, delta: maxInt64, limit: maxInt64, want: maxInt64, wantOK: true},
		{name: "max overflow", current: maxInt64, delta: 1, limit: maxInt64, want: maxInt64, wantOK: false},
		{name: "negative current", current: -1, delta: 0, limit: 1, want: -1, wantOK: false},
		{name: "negative delta", current: 0, delta: -1, limit: 1, want: 0, wantOK: false},
		{name: "negative limit", current: 0, delta: 0, limit: -1, want: 0, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := checkedAdd(tc.current, tc.delta, tc.limit)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("checkedAdd(%d, %d, %d) = (%d, %v), want (%d, %v)", tc.current, tc.delta, tc.limit, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestRequestErrorKindMatrixIsTypedAndMapped(t *testing.T) {
	kinds := []RequestErrorKind{
		RequestErrorMalformed,
		RequestErrorMalformedJSON,
		RequestErrorBodyRead,
		RequestErrorCSRF,
		RequestErrorMethod,
		RequestErrorUnsupportedMedia,
		RequestErrorBodyTooLarge,
		RequestErrorFieldsTooLarge,
		RequestErrorTooManyFields,
		RequestErrorFieldNameTooLarge,
		RequestErrorMetadataTooLarge,
		RequestErrorFilesNotAllowed,
		RequestErrorTooManyFiles,
		RequestErrorFileTooLarge,
		RequestErrorMissingUpload,
		RequestErrorMultipleUpload,
		RequestErrorMissingPolicy,
		RequestErrorTempCreate,
		RequestErrorTempWrite,
		RequestErrorTempClose,
		RequestErrorTempOpen,
		RequestErrorTempRemove,
		RequestErrorUploadOpen,
		RequestErrorHandler,
		RequestErrorSerialization,
		RequestErrorResultValuesOverBudget,
		RequestErrorUnknown,
	}
	cause := errors.New("cause")
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			err := requestError(kind, cause)
			if err.Kind() != kind {
				t.Fatalf("kind=%q, want %q", err.Kind(), kind)
			}
			if err.StatusCode() == 0 || err.ManagedCode() == "" {
				t.Fatalf("kind=%q has incomplete status/code: %d/%q", kind, err.StatusCode(), err.ManagedCode())
			}
			if !errors.Is(err, cause) {
				t.Fatal("request error dropped its cause")
			}
		})
	}
	unknown := requestError(RequestErrorKind("not-a-public-kind"), nil)
	if unknown.Kind() != RequestErrorUnknown || unknown.StatusCode() != http.StatusInternalServerError || unknown.ManagedCode() != "internal_error" {
		t.Fatalf("unknown request error = kind=%q status=%d code=%q", unknown.Kind(), unknown.StatusCode(), unknown.ManagedCode())
	}
}

type testUploadTemp struct {
	bytes.Buffer
	name   string
	closed bool
}

func (f *testUploadTemp) Name() string { return f.name }

func (f *testUploadTemp) Close() error {
	f.closed = true
	return nil
}

func TestMultipartStorageFailuresKeepTypedBoundaries(t *testing.T) {
	createErr := errors.New("create failed")
	writeErr := errors.New("write failed")
	closeErr := errors.New("close failed")
	openErr := errors.New("open failed")
	readerCloseErr := errors.New("reader close failed")
	removeErr := errors.New("remove failed")
	newStorage := func() uploadStorageHooks {
		storage := defaultUploadStorage
		storage.createTemp = func() (uploadTempFile, error) {
			return &testUploadTemp{name: "/tmp/gosx-test-upload"}, nil
		}
		storage.remove = func(string) error { return nil }
		return storage
	}
	read := func(t *testing.T, storage uploadStorageHooks) (Upload, *RequestError) {
		t.Helper()
		memoryUsed := int64(0)
		return readMultipartUpload(strings.NewReader("spill-data"), "avatar", "avatar.bin", "application/octet-stream", Limits{MaxFileBytes: 64, MaxMemoryFileBytes: 1}, &memoryUsed, storage)
	}

	t.Run("create", func(t *testing.T) {
		storage := newStorage()
		storage.createTemp = func() (uploadTempFile, error) { return nil, createErr }
		_, err := read(t, storage)
		assertRequestErrorKind(t, err, RequestErrorTempCreate, createErr)
	})
	t.Run("short-write", func(t *testing.T) {
		storage := newStorage()
		storage.write = func(io.Writer, []byte) error { return writeErr }
		_, err := read(t, storage)
		assertRequestErrorKind(t, err, RequestErrorTempWrite, writeErr)
	})
	t.Run("close", func(t *testing.T) {
		storage := newStorage()
		storage.close = func(io.Closer) error { return closeErr }
		_, err := read(t, storage)
		assertRequestErrorKind(t, err, RequestErrorTempClose, closeErr)
	})
	t.Run("open", func(t *testing.T) {
		storage := newStorage()
		storage.open = func(string) (io.ReadCloser, error) { return nil, openErr }
		upload, err := read(t, storage)
		if err != nil {
			t.Fatal(err)
		}
		_, openFailure := upload.Open()
		assertRequestErrorKind(t, openFailure, RequestErrorTempOpen, openErr)
		_ = upload.state.cleanup()
	})
	t.Run("reader-close", func(t *testing.T) {
		storage := newStorage()
		storage.open = func(string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("ok")), nil }
		var closeCalls int
		storage.close = func(io.Closer) error {
			closeCalls++
			if closeCalls > 1 {
				return readerCloseErr
			}
			return nil
		}
		upload, err := read(t, storage)
		if err != nil {
			t.Fatal(err)
		}
		reader, openFailure := upload.Open()
		if openFailure != nil {
			t.Fatal(openFailure)
		}
		if err := reader.Close(); !errors.Is(err, readerCloseErr) {
			t.Fatalf("reader Close error=%v, want %v", err, readerCloseErr)
		}
		_ = upload.state.cleanup()
	})
	t.Run("remove", func(t *testing.T) {
		storage := newStorage()
		storage.remove = func(string) error { return removeErr }
		upload, err := read(t, storage)
		if err != nil {
			t.Fatal(err)
		}
		cleanupErr := upload.state.cleanup()
		assertRequestErrorKind(t, cleanupErr, RequestErrorTempRemove, removeErr)
	})
	t.Run("combined-reader-close-remove", func(t *testing.T) {
		storage := newStorage()
		storage.open = func(string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("ok")), nil }
		var closeCalls int
		storage.close = func(io.Closer) error {
			closeCalls++
			if closeCalls > 1 {
				return readerCloseErr
			}
			return nil
		}
		storage.remove = func(string) error { return removeErr }
		upload, err := read(t, storage)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := upload.Open(); err != nil {
			t.Fatal(err)
		}
		cleanupErr := upload.state.cleanup()
		if !errors.Is(cleanupErr, readerCloseErr) || !errors.Is(cleanupErr, removeErr) {
			t.Fatalf("combined cleanup error=%v, want reader and remove causes", cleanupErr)
		}
		var tempRemove *RequestError
		if !errors.As(cleanupErr, &tempRemove) || tempRemove.Kind() != RequestErrorTempRemove {
			t.Fatalf("combined cleanup error=%v, missing typed temp remove", cleanupErr)
		}
	})
}

func assertRequestErrorKind(t *testing.T, err error, want RequestErrorKind, cause error) {
	t.Helper()
	if err == nil {
		t.Fatalf("error=nil, want %q", want)
	}
	var requestErr *RequestError
	if !errors.As(err, &requestErr) {
		t.Fatalf("error=%T %v is not a RequestError", err, err)
	}
	if requestErr.Kind() != want {
		t.Fatalf("kind=%q, want %q", requestErr.Kind(), want)
	}
	if cause != nil && !errors.Is(err, cause) {
		t.Fatalf("error=%v does not wrap %v", err, cause)
	}
}
