package docs

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestTabletopUsesDeclarativeSceneStartPolicyAndFrameCap(t *testing.T) {
	tabletop, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		`startPolicy="idle-visible-hardware"`,
		`maxFrameRate={30}`,
		`respectReducedMotion={true}`,
		"unsupported GPU keeps the server-rendered poster",
	} {
		if !strings.Contains(string(tabletop), required) {
			t.Errorf("tabletop page is missing declarative startup contract %q", required)
		}
	}
	if strings.Contains(string(tabletop), `startPolicy="hardware-auto-software-manual"`) || strings.Contains(string(tabletop), `data-gosx-scene3d-start=`) {
		t.Fatal("tabletop must keep the poster when no hardware GPU is available")
	}
}

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

func TestPrivateRoomRedirectDoesNotReserveRoom(t *testing.T) {
	previous := Rooms
	Rooms = NewRoomManager()
	t.Cleanup(func() { Rooms = previous })

	next := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/demos/tabletop", nil)
	PrivateRoomRedirect(next).ServeHTTP(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("private room response = %d, want %d", response.Code, http.StatusFound)
	}
	target, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	roomID := target.Query().Get("room")
	if !validTabletopRoomID(roomID) {
		t.Fatalf("private room redirect used invalid room id %q", roomID)
	}
	if got := Rooms.Count(); got != 0 {
		t.Fatalf("private room redirect reserved %d rooms", got)
	}
}
