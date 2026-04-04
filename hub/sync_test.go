package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/odvcencio/gosx/crdt"
	crdtsync "github.com/odvcencio/gosx/crdt/sync"
)

func TestHubSyncDocBootstrapsAndAppliesBinaryChanges(t *testing.T) {
	h := New("collab")
	doc := crdt.NewDoc()
	if err := doc.Put(crdt.Root, "title", crdt.StringValue("server")); err != nil {
		t.Fatalf("seed server doc: %v", err)
	}
	if _, err := doc.Commit("seed"); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
	h.SyncDoc("room-123", doc)

	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	var (
		welcomeSeen bool
		syncData    []byte
	)
	for i := 0; i < 2; i++ {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read bootstrap message %d: %v", i, err)
		}
		switch msgType {
		case websocket.TextMessage:
			var welcome Message
			if err := json.Unmarshal(data, &welcome); err != nil {
				t.Fatalf("decode welcome: %v", err)
			}
			if welcome.Event == "__welcome" {
				welcomeSeen = true
			}
		case websocket.BinaryMessage:
			syncData = append([]byte(nil), data...)
		default:
			t.Fatalf("unexpected bootstrap message type %d", msgType)
		}
	}
	if !welcomeSeen {
		t.Fatal("expected welcome message during bootstrap")
	}
	if len(syncData) < 2 {
		t.Fatalf("expected prefixed binary sync payload, got %d bytes", len(syncData))
	}

	prefix := syncData[0]
	clientDoc := crdt.NewDoc()
	clientState := crdtsync.NewState()
	if err := clientDoc.ReceiveSyncMessage(clientState, syncData[1:]); err != nil {
		t.Fatalf("apply bootstrap sync: %v", err)
	}

	value, _, err := clientDoc.Get(crdt.Root, "title")
	if err != nil {
		t.Fatalf("get synced title: %v", err)
	}
	if value.Str != "server" {
		t.Fatalf("expected synced title server, got %q", value.Str)
	}

	if err := clientDoc.Put(crdt.Root, "subtitle", crdt.StringValue("client")); err != nil {
		t.Fatalf("client subtitle put: %v", err)
	}
	if _, err := clientDoc.Commit("client update"); err != nil {
		t.Fatalf("commit client update: %v", err)
	}
	clientMsg, ok := clientDoc.GenerateSyncMessage(clientState)
	if !ok {
		t.Fatal("expected client sync message after local change")
	}

	if err := conn.WriteMessage(websocket.BinaryMessage, append([]byte{prefix}, clientMsg...)); err != nil {
		t.Fatalf("write client sync message: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	value, _, err = doc.Get(crdt.Root, "subtitle")
	if err != nil {
		t.Fatalf("get server subtitle: %v", err)
	}
	if value.Str != "client" {
		t.Fatalf("expected server subtitle client, got %q", value.Str)
	}
}
