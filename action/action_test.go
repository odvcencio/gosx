package action

import (
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
	r.Register("greet", func(ctx *Context) error { return nil })

	req := httptest.NewRequest("POST", "/gosx/action/greet", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
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
