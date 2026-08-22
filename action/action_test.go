package action

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx/session"
)

func TestRegistryRegisterAndInvoke(t *testing.T) {
	r := NewRegistry()
	called := false
	r.Register("test", func(ctx *Context) error {
		called = true
		return nil
	})

	if !r.Has("test") {
		t.Fatal("expected handler to be registered")
	}

	err := r.Invoke("test", &Context{})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("handler not called")
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	r.Register("a", func(ctx *Context) error { return nil })
	r.Register("b", func(ctx *Context) error { return nil })

	list := r.List()
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
}

func TestRegistryHTTP(t *testing.T) {
	r := NewRegistry()
	r.Register("greet", func(ctx *Context) error {
		if string(ctx.Payload) != `{}` {
			t.Fatalf("expected payload to be decoded, got %s", string(ctx.Payload))
		}
		return nil
	})

	req := httptest.NewRequest("POST", "/gosx/action/greet", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", "greet")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRegistryHTTPContentTypeCharset(t *testing.T) {
	r := NewRegistry()
	r.Register("greet", func(ctx *Context) error {
		if string(ctx.Payload) != `{"message":"hi"}` {
			t.Fatalf("expected payload to be decoded, got %s", string(ctx.Payload))
		}
		return nil
	})

	req := httptest.NewRequest("POST", "/gosx/action/greet", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.SetPathValue("name", "greet")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRegistryMissingHandler(t *testing.T) {
	r := NewRegistry()
	err := r.Invoke("missing", &Context{})
	if err == nil {
		t.Fatal("expected error for missing handler")
	}
}

func TestRegistryHTTPNotFound(t *testing.T) {
	r := NewRegistry()

	req := httptest.NewRequest("POST", "/gosx/action/missing", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", "missing")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestRegistryHTTPMethodNotAllowed(t *testing.T) {
	r := NewRegistry()
	r.Register("greet", func(ctx *Context) error { return nil })

	req := httptest.NewRequest("GET", "/gosx/action/greet", nil)
	req.SetPathValue("name", "greet")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestRegistryHTTPFallbackPathExtraction(t *testing.T) {
	r := NewRegistry()
	r.Register("greet", func(ctx *Context) error { return nil })

	req := httptest.NewRequest("POST", "/gosx/action/greet", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRegistryHTTPInvalidJSON(t *testing.T) {
	r := NewRegistry()
	r.Register("greet", func(ctx *Context) error { return nil })

	req := httptest.NewRequest("POST", "/gosx/action/greet", strings.NewReader(`{"broken"`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", "greet")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRegistryHTTPOversizedJSON(t *testing.T) {
	r := NewRegistry()
	r.Register("greet", func(ctx *Context) error { return nil })

	body, err := json.Marshal(map[string]string{
		"payload": strings.Repeat("a", maxActionBodyBytes),
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/gosx/action/greet", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", "greet")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", w.Code)
	}
}

func TestRegistryHTTPOversizedForm(t *testing.T) {
	r := NewRegistry()
	r.Register("submit", func(ctx *Context) error { return nil })

	form := "name=" + strings.Repeat("a", maxActionBodyBytes+1)
	req := httptest.NewRequest("POST", "/gosx/action/submit", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "submit")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", w.Code)
	}
}

func TestRegistryHTTPFormData(t *testing.T) {
	r := NewRegistry()
	r.Register("submit", func(ctx *Context) error {
		if got := ctx.FormData["name"]; got != "Ada" {
			t.Fatalf("expected form value Ada, got %q", got)
		}
		return nil
	})

	req := httptest.NewRequest("POST", "/gosx/action/submit", strings.NewReader("name=Ada"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "submit")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRegistryHTTPMultipartFormData(t *testing.T) {
	r := NewRegistry()
	r.Register("submit", func(ctx *Context) error {
		if got := ctx.FormData["name"]; got != "Ada" {
			t.Fatalf("expected multipart form value Ada, got %q", got)
		}
		if got := ctx.FormData["path"]; got != "power" {
			t.Fatalf("expected multipart form value power, got %q", got)
		}
		return nil
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("name", "Ada"); err != nil {
		t.Fatalf("write name field: %v", err)
	}
	if err := writer.WriteField("path", "power"); err != nil {
		t.Fatalf("write path field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest("POST", "/gosx/action/submit", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.SetPathValue("name", "submit")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestContextFilesFromMultipartForm(t *testing.T) {
	var gotFiles []*multipart.FileHeader
	var gotFile *multipart.FileHeader
	var gotMissing []*multipart.FileHeader

	r := NewRegistry()
	r.Register("upload", func(ctx *Context) error {
		gotFiles = ctx.Files("avatar")
		gotFile = ctx.File("avatar")
		gotMissing = ctx.Files("does-not-exist")
		return nil
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("name", "Ada"); err != nil {
		t.Fatalf("write name field: %v", err)
	}
	part, err := writer.CreateFormFile("avatar", "avatar.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("fake-png-bytes")); err != nil {
		t.Fatalf("write file bytes: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest("POST", "/gosx/action/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.SetPathValue("name", "upload")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if len(gotFiles) != 1 {
		t.Fatalf("expected 1 file header, got %d", len(gotFiles))
	}
	if gotFiles[0].Filename != "avatar.png" {
		t.Fatalf("expected avatar.png, got %q", gotFiles[0].Filename)
	}
	if gotFile == nil || gotFile.Filename != "avatar.png" {
		t.Fatalf("expected File convenience accessor to return avatar.png, got %#v", gotFile)
	}
	if gotMissing != nil {
		t.Fatalf("expected nil for an absent field name, got %#v", gotMissing)
	}
}

func TestContextFilesNilOnNonMultipartRequest(t *testing.T) {
	var gotFiles []*multipart.FileHeader
	var gotFile *multipart.FileHeader

	r := NewRegistry()
	r.Register("submit", func(ctx *Context) error {
		gotFiles = ctx.Files("avatar")
		gotFile = ctx.File("avatar")
		return nil
	})

	req := httptest.NewRequest("POST", "/gosx/action/submit", strings.NewReader("name=Ada"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "submit")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if gotFiles != nil {
		t.Fatalf("expected nil Files on a non-multipart request, got %#v", gotFiles)
	}
	if gotFile != nil {
		t.Fatalf("expected nil File on a non-multipart request, got %#v", gotFile)
	}
}

func TestContextFilesNilSafeOnNilContext(t *testing.T) {
	var ctx *Context
	if got := ctx.Files("avatar"); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
	if got := ctx.File("avatar"); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}

func TestServeHandlerWithOptionsRejectsOversizedMultipartUpload(t *testing.T) {
	called := false
	handler := func(ctx *Context) error {
		called = true
		return nil
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("avatar", "avatar.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("a"), 4096)); err != nil {
		t.Fatalf("write file bytes: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest("POST", "/gosx/action/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	ServeHandlerWithOptions(w, req, handler, ServeHandlerOptions{MaxBodyBytes: 1024})

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body.String())
	}
	if called {
		t.Fatal("expected the handler not to run once the body exceeds the configured limit")
	}
}

func TestServeHandlerWithOptionsAllowsUploadUnderConfiguredLimit(t *testing.T) {
	var uploaded *multipart.FileHeader
	handler := func(ctx *Context) error {
		uploaded = ctx.File("avatar")
		return ctx.Success("saved", nil)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("avatar", "avatar.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	// The payload sits over the package's 1 MiB default cap and under the 2
	// MiB cap configured below, so this proves the knob raises the ceiling
	// rather than merely staying under an unrelated limit.
	payload := bytes.Repeat([]byte("a"), maxActionBodyBytes+512*1024)
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("write file bytes: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest("POST", "/gosx/action/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	ServeHandlerWithOptions(w, req, handler, ServeHandlerOptions{MaxBodyBytes: 2 * 1024 * 1024})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if uploaded == nil || uploaded.Filename != "avatar.png" {
		t.Fatalf("expected uploaded avatar.png, got %#v", uploaded)
	}
}

func TestServeHandlerWithOptionsZeroFallsBackToPackageDefault(t *testing.T) {
	handler := func(ctx *Context) error { return nil }

	body := "name=" + strings.Repeat("a", maxActionBodyBytes+1)
	req := httptest.NewRequest("POST", "/gosx/action/submit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	ServeHandlerWithOptions(w, req, handler, ServeHandlerOptions{})

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 from the package default cap, got %d", w.Code)
	}
}

func TestWantsJSON(t *testing.T) {
	tests := []struct {
		name          string
		contentType   string
		accept        string
		requestedWith string
		nilRequest    bool
		want          bool
	}{
		{name: "nil request", nilRequest: true, want: true},
		{name: "JSON Accept", accept: "application/json", want: true},
		{name: "JSON content type", contentType: "application/json; charset=utf-8", want: true},
		{name: "managed X-Requested-With", requestedWith: "XMLHttpRequest", want: true},
		{
			name:        "native form",
			contentType: "application/x-www-form-urlencoded",
			accept:      "text/html,application/xhtml+xml",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if !tt.nilRequest {
				req = httptest.NewRequest(http.MethodPost, "/account/__actions/save", nil)
				req.Header.Set("Content-Type", tt.contentType)
				req.Header.Set("Accept", tt.accept)
				req.Header.Set("X-Requested-With", tt.requestedWith)
			}
			if got := WantsJSON(req); got != tt.want {
				t.Fatalf("WantsJSON() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegistryHTTPStructuredValidationError(t *testing.T) {
	r := NewRegistry()
	r.Register("submit", func(ctx *Context) error {
		return Validation("name is required", map[string]string{"name": "required"}, ctx.FormData)
	})

	req := httptest.NewRequest("POST", "/gosx/action/submit", strings.NewReader("name="))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "submit")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", w.Code)
	}

	var result Result
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.OK {
		t.Fatal("expected failed result")
	}
	if result.FieldErrors["name"] != "required" {
		t.Fatalf("expected field error, got %#v", result.FieldErrors)
	}
}

func TestServeHandlerFlashesBrowserFormStateWhenSessionPresent(t *testing.T) {
	sessions := session.MustNew("action-session-test-secret", session.Options{})
	handler := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			ServeHandler(w, r, func(ctx *Context) error {
				return Validation("email is required", map[string]string{
					"email": "required",
				}, ctx.FormData)
			})
			return
		}

		view, ok := State(r, "save")
		if !ok {
			t.Fatal("expected flashed action state")
		}
		if !view.HasError("email") || view.Value("name") != "Ada" {
			t.Fatalf("unexpected flashed view %#v", view)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	postReq := httptest.NewRequest(http.MethodPost, "/account/__actions/save", strings.NewReader("name=Ada&email="))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRes := httptest.NewRecorder()
	handler.ServeHTTP(postRes, postReq)
	if postRes.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", postRes.Code)
	}
	if location := postRes.Header().Get("Location"); location != "/account" {
		t.Fatalf("expected redirect to page path, got %q", location)
	}
	cookie := postRes.Result().Cookies()[0]

	getReq := httptest.NewRequest(http.MethodGet, "/account", nil)
	getReq.AddCookie(cookie)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", getRes.Code)
	}
}

func TestRegistryHTTPContextRedirect(t *testing.T) {
	r := NewRegistry()
	r.Register("submit", func(ctx *Context) error {
		ctx.Redirect("/users")
		return nil
	})

	req := httptest.NewRequest("POST", "/gosx/action/submit", strings.NewReader("name=Ada"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("name", "submit")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/users" {
		t.Fatalf("expected redirect to /users, got %q", got)
	}
}

func TestRegistryHTTPContextRedirectWithMessageUsesOneResultForManagedAndNativeForms(t *testing.T) {
	r := NewRegistry()
	r.Register("submit", func(ctx *Context) error {
		ctx.RedirectWithMessage("/users", "  Profile saved.  ")
		return nil
	})

	jsonReq := httptest.NewRequest(http.MethodPost, "/gosx/action/submit", strings.NewReader("name=Ada"))
	jsonReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	jsonReq.Header.Set("Accept", "application/json")
	jsonReq.SetPathValue("name", "submit")
	jsonRes := httptest.NewRecorder()
	r.ServeHTTP(jsonRes, jsonReq)
	if jsonRes.Code != http.StatusSeeOther {
		t.Fatalf("managed response status = %d, want %d", jsonRes.Code, http.StatusSeeOther)
	}
	var got Result
	if err := json.NewDecoder(jsonRes.Body).Decode(&got); err != nil {
		t.Fatalf("decode managed response: %v", err)
	}
	if !got.OK || got.Message != "Profile saved." || got.Redirect != "/users" {
		t.Fatalf("managed response = %+v", got)
	}

	nativeReq := httptest.NewRequest(http.MethodPost, "/gosx/action/submit", strings.NewReader("name=Ada"))
	nativeReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	nativeReq.SetPathValue("name", "submit")
	nativeRes := httptest.NewRecorder()
	r.ServeHTTP(nativeRes, nativeReq)
	if nativeRes.Code != http.StatusSeeOther {
		t.Fatalf("native response status = %d, want %d", nativeRes.Code, http.StatusSeeOther)
	}
	if got := nativeRes.Header().Get("Location"); got != "/users" {
		t.Fatalf("native redirect = %q, want /users", got)
	}
}

func TestRegistryHTTPFormRedirectsBackOnSuccess(t *testing.T) {
	r := NewRegistry()
	r.Register("submit", func(ctx *Context) error {
		return nil
	})

	req := httptest.NewRequest("POST", "/gosx/action/submit", strings.NewReader("name=Ada"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "/users/new")
	req.SetPathValue("name", "submit")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/users/new" {
		t.Fatalf("expected redirect back to referer, got %q", got)
	}
}

func TestRegistryHTTPSuccessResultJSON(t *testing.T) {
	r := NewRegistry()
	r.Register("submit", func(ctx *Context) error {
		return ctx.Success("saved", map[string]any{"id": 7})
	})

	req := httptest.NewRequest("POST", "/gosx/action/submit", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", "submit")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result Result
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !result.OK || result.Message != "saved" {
		t.Fatalf("unexpected result %#v", result)
	}
	if string(result.Data) != `{"id":7}` {
		t.Fatalf("unexpected data %s", result.Data)
	}
}

func TestFormValues(t *testing.T) {
	fv := NewFormValues(map[string]string{"key": "val"})

	if fv.Get("key") != "val" {
		t.Fatalf("expected val, got %q", fv.Get("key"))
	}
	if !fv.Has("key") {
		t.Fatal("expected Has to return true")
	}
	if fv.Has("missing") {
		t.Fatal("expected Has to return false for missing key")
	}

	all := fv.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(all))
	}
}
