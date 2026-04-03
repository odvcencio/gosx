package markdown

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGrammarBlobHandler_ServesGoGrammar(t *testing.T) {
	handler := GrammarBlobHandler()
	req := httptest.NewRequest(http.MethodGet, "/grammars/go.blob", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty body for go grammar blob")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("expected Content-Type application/octet-stream, got %q", ct)
	}
}

func TestGrammarBlobHandler_404ForUnknown(t *testing.T) {
	handler := GrammarBlobHandler()
	req := httptest.NewRequest(http.MethodGet, "/grammars/nonexistent.blob", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGrammarBlobHandler_RejectsDotDot(t *testing.T) {
	handler := GrammarBlobHandler()
	req := httptest.NewRequest(http.MethodGet, "/grammars/../../../etc/passwd.blob", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for path traversal, got %d", rec.Code)
	}
}

func TestGrammarBlobHandler_CacheHeaders(t *testing.T) {
	handler := GrammarBlobHandler()
	req := httptest.NewRequest(http.MethodGet, "/grammars/go.blob", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	cc := rec.Header().Get("Cache-Control")
	if cc != "public, max-age=31536000, immutable" {
		t.Errorf("expected Cache-Control 'public, max-age=31536000, immutable', got %q", cc)
	}

	cors := rec.Header().Get("Access-Control-Allow-Origin")
	if cors != "*" {
		t.Errorf("expected Access-Control-Allow-Origin '*', got %q", cors)
	}
}
