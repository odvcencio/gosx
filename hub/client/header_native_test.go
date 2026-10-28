//go:build !(js && wasm)

package hubclient

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Options.Header is a plain map so the js/wasm build does not import net/http.
// Callers that assign an http.Header to it must still compile, because both
// types share an underlying type. Callers that call an http.Header method on
// the field (Set, Add, Get) cannot compile; Options.Header says how to migrate.
// This declaration stops the package's tests from building if an http.Header
// value ever stops assigning to the field.
var _ = Options{Header: http.Header{}}

func TestNativeDialSendsOptionsHeader(t *testing.T) {
	// The migration path for code that used Set or Add on the field: fill an
	// http.Header, then assign it.
	filled := http.Header{}
	filled.Set("X-Gosx-Single", "alpha")
	filled.Add("X-Gosx-Multi", "one")
	filled.Add("X-Gosx-Multi", "two")
	for name, header := range map[string]map[string][]string{
		"filled http.Header": filled,
		"map literal":        {"X-Gosx-Single": {"alpha"}, "X-Gosx-Multi": {"one", "two"}},
	} {
		t.Run(name, func(t *testing.T) {
			got := upgradeRequestHeader(t, header)
			if v := got["X-Gosx-Single"]; !slices.Equal(v, []string{"alpha"}) {
				t.Errorf("upgrade request X-Gosx-Single = %q, want [alpha]", v)
			}
			if v := got["X-Gosx-Multi"]; !slices.Equal(v, []string{"one", "two"}) {
				t.Errorf("upgrade request X-Gosx-Multi = %q, want [one two]", v)
			}
		})
	}
}

// upgradeRequestHeader dials a throwaway server with header in Options.Header
// and returns the headers its WebSocket upgrade request carried.
func upgradeRequestHeader(t *testing.T, header map[string][]string) http.Header {
	t.Helper()
	seen := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer ws.Close()
		select {
		case seen <- r.Header.Clone():
		default:
		}
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	c := New(Options{URL: wsURL(server.URL), Header: header})
	c.Connect()
	defer closeClient(t, c)
	select {
	case got := <-seen:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive the upgrade request")
		return nil
	}
}
