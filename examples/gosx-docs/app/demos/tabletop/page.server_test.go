package docs

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoomRouteMiddlewareBypassesISRAndNormalizesSlash(t *testing.T) {
	var bypass string
	next := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		bypass = req.Header.Get("X-GoSX-ISR-Revalidate")
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RoomRouteMiddleware(next)

	request := httptest.NewRequest(http.MethodGet, "/demos/tabletop?room=feedface12345678", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || bypass != "tabletop-room" {
		t.Fatalf("dynamic room response = %d with ISR signal %q", response.Code, bypass)
	}

	bypass = ""
	request = httptest.NewRequest(http.MethodGet, "/demos/tabletop/?room=feedface12345678", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusPermanentRedirect || response.Header().Get("Location") != "/demos/tabletop?room=feedface12345678" {
		t.Fatalf("slash alias response = %d to %q", response.Code, response.Header().Get("Location"))
	}
	if bypass != "" {
		t.Fatalf("slash alias reached next handler with ISR signal %q", bypass)
	}

	request = httptest.NewRequest(http.MethodGet, "/demos/scene3d", nil)
	response = httptest.NewRecorder()
	bypass = ""
	handler.ServeHTTP(response, request)
	if bypass != "" {
		t.Fatalf("unrelated route got ISR signal %q", bypass)
	}
}
