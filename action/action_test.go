package action

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
