package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestServePublicAppliesSourceBundlePolicy(t *testing.T) {
	project := t.TempDir()
	public := filepath.Join(project, "public")
	if err := os.MkdirAll(public, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(public, "state.db"), []byte("public state"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(public, ".env"), []byte("TOKEN=secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "gosx.config.json"), []byte(`{"build":{"hooks":{"pre":[]},"bundle":{"allowPublic":["public/state.db"]}}}`), 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetPublicDir(public)
	handler := app.Build()

	allowed := httptest.NewRecorder()
	handler.ServeHTTP(allowed, httptest.NewRequest(http.MethodGet, "/state.db", nil))
	if allowed.Code != http.StatusOK || allowed.Body.String() != "public state" {
		t.Fatalf("allowPublic response = %d %q", allowed.Code, allowed.Body.String())
	}

	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/.env", nil))
	if denied.Code == http.StatusOK || denied.Body.String() == "TOKEN=secret" {
		t.Fatalf("secret was served: %d %q", denied.Code, denied.Body.String())
	}

	traversal := httptest.NewRecorder()
	handler.ServeHTTP(traversal, httptest.NewRequest(http.MethodGet, "/../state.db", nil))
	if traversal.Code == http.StatusOK {
		t.Fatalf("path traversal was served: %d %q", traversal.Code, traversal.Body.String())
	}
}

func TestServePublicFailsClosedForMalformedBundlePolicy(t *testing.T) {
	root := t.TempDir()
	public := filepath.Join(root, "public")
	if err := os.MkdirAll(public, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(public, "private.txt"), []byte("must not be served"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bundle-policy.json"), []byte("{not-json"), 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetPublicDir(public)
	recorder := httptest.NewRecorder()
	app.Build().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/private.txt", nil))
	if recorder.Code == http.StatusOK || recorder.Body.String() == "must not be served" {
		t.Fatalf("malformed staged policy failed open: %d %q", recorder.Code, recorder.Body.String())
	}
}
