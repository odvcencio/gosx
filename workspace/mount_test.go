package workspace

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkspaceIsHTTPHandler(t *testing.T) {
	ws := New(Options{Name: "test", Dim: 4, BitWidth: 2})

	var _ http.Handler = ws

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/workspace/test", nil)
	ws.ServeHTTP(rec, req)
}

func TestWorkspaceRegisterEvents(t *testing.T) {
	ws := New(Options{Name: "test", Dim: 4, BitWidth: 2})
	h := ws.Hub()
	if h == nil {
		t.Fatal("hub is nil")
	}
}
