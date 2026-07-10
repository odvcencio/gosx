package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/crdt"
	crdtsync "m31labs.dev/gosx/crdt/sync"
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

// TestHubBinaryAuthorizerDropsUnauthorizedInboundSync is the regression guard
// for the inbound-binary-sync bypass of a hub-level write gate: without a
// BinaryAuthorizer, ANY connected client — including one that a caller (e.g.
// kiln's collab.Session) has resolved as read-only over the JSON event
// protocol — can still push an inbound binary CRDT sync frame and have it
// applied server-side, because handleBinaryMessage previously had no
// per-client authorization hook at all. This asserts that once a
// BinaryAuthorizer is installed and denies a client, that client's inbound
// sync frame is dropped (the server document is unchanged), while an
// authorized client's inbound sync is still applied, and while a denied
// client still RECEIVES server -> client broadcasts (the gate is
// inbound-only).
func TestHubBinaryAuthorizerDropsUnauthorizedInboundSync(t *testing.T) {
	h := New("collab-auth")
	doc := crdt.NewDoc()
	if err := doc.Put(crdt.Root, "title", crdt.StringValue("server")); err != nil {
		t.Fatalf("seed server doc: %v", err)
	}
	if _, err := doc.Commit("seed"); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
	h.SyncDoc("room-auth", doc)

	server := httptest.NewServer(h)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// dial connects, drains the two bootstrap frames (__welcome + the initial
	// binary sync payload), and returns the connection, its hub-assigned
	// client ID (from __welcome), the SyncDoc's wire prefix byte, and the
	// bootstrap payload's CRDT bytes (everything after the prefix byte).
	dial := func() (conn *websocket.Conn, clientID string, prefix byte, bootstrap []byte) {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{})
		if err != nil {
			t.Fatalf("dial websocket: %v", err)
		}
		var sawPrefix bool
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
					var payload struct {
						ClientID string `json:"clientId"`
					}
					json.Unmarshal(welcome.Data, &payload)
					clientID = payload.ClientID
				}
			case websocket.BinaryMessage:
				if len(data) < 1 {
					t.Fatalf("bootstrap binary frame too short: %d bytes", len(data))
				}
				prefix = data[0]
				bootstrap = append([]byte(nil), data[1:]...)
				sawPrefix = true
			}
		}
		if clientID == "" {
			t.Fatal("did not observe __welcome clientId")
		}
		if !sawPrefix {
			t.Fatal("did not observe a prefixed binary sync bootstrap frame")
		}
		return conn, clientID, prefix, bootstrap
	}

	viewerConn, viewerID, prefix, viewerBootstrap := dial()
	defer viewerConn.Close()
	writerConn, writerID, _, _ := dial()
	defer writerConn.Close()

	// viewerReplica is a persistent client-side replica of the doc, seeded
	// from the viewer's own bootstrap frame and updated in place as further
	// broadcast frames arrive — modeling how a real client applies
	// incremental sync messages (each broadcast carries only the delta since
	// that client's own last-acknowledged state, not the full history).
	viewerReplica := crdt.NewDoc()
	viewerReplicaState := crdtsync.NewState()
	if err := viewerReplica.ReceiveSyncMessage(viewerReplicaState, viewerBootstrap); err != nil {
		t.Fatalf("bootstrap viewer replica: %v", err)
	}

	// Only the connection whose hub-assigned ID matches writerID may push
	// inbound sync — stand-in for kiln's collab.Session gating on
	// Client.CanWrite, resolved per-connection at join time.
	var authMu sync.Mutex
	var authCalls []string
	h.SetBinaryAuthorizer(func(client *Client, docName string) bool {
		authMu.Lock()
		authCalls = append(authCalls, client.ID+":"+docName)
		authMu.Unlock()
		return docName == "room-auth" && client.ID == writerID
	})

	// sendMutation builds a fresh client-side replica bootstrapped from the
	// CURRENT server doc, sets field=value locally, and pushes the resulting
	// sync message inbound over conn.
	sendMutation := func(conn *websocket.Conn, field, value string) {
		t.Helper()
		bootstrapState := crdtsync.NewState()
		bootstrapMsg, ok := doc.GenerateSyncMessage(bootstrapState)
		if !ok {
			t.Fatal("expected server sync message to bootstrap client replica")
		}
		clientDoc := crdt.NewDoc()
		clientState := crdtsync.NewState()
		if err := clientDoc.ReceiveSyncMessage(clientState, bootstrapMsg); err != nil {
			t.Fatalf("bootstrap client replica: %v", err)
		}
		if err := clientDoc.Put(crdt.Root, crdt.Prop(field), crdt.StringValue(value)); err != nil {
			t.Fatalf("client put %s: %v", field, err)
		}
		if _, err := clientDoc.Commit("client mutation"); err != nil {
			t.Fatalf("commit client mutation: %v", err)
		}
		clientMsg, ok := clientDoc.GenerateSyncMessage(clientState)
		if !ok {
			t.Fatal("expected client sync message after local change")
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, append([]byte{prefix}, clientMsg...)); err != nil {
			t.Fatalf("write client sync message: %v", err)
		}
	}

	// Unauthorized (viewer) client's inbound sync is dropped: the server
	// document never gets "poisoned".
	sendMutation(viewerConn, "poisoned", "from-viewer")
	time.Sleep(150 * time.Millisecond)
	if _, _, err := doc.Get(crdt.Root, "poisoned"); err == nil {
		t.Fatal("unauthorized client's inbound sync frame was applied to the server document")
	}

	// The viewer should have received a __crdt_error frame in response.
	sawError := false
	viewerConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	for i := 0; i < 5; i++ {
		msgType, data, err := viewerConn.ReadMessage()
		if err != nil {
			break
		}
		if msgType != websocket.TextMessage {
			continue
		}
		var msg Message
		if json.Unmarshal(data, &msg) == nil && msg.Event == "__crdt_error" {
			sawError = true
			break
		}
	}
	if !sawError {
		t.Fatal("expected unauthorized client to receive a __crdt_error frame")
	}

	// Authorized (writer) client's inbound sync IS applied.
	sendMutation(writerConn, "approved", "from-writer")
	time.Sleep(150 * time.Millisecond)
	value, _, err := doc.Get(crdt.Root, "approved")
	if err != nil {
		t.Fatalf("get server approved field: %v", err)
	}
	if value.Str != "from-writer" {
		t.Fatalf("expected server approved=from-writer, got %q", value.Str)
	}

	// The unauthorized (viewer) client still RECEIVES server -> client
	// broadcasts: the writer's change above triggers broadcastSyncDoc to
	// every client, including the viewer, since the authorizer only gates
	// inbound application.
	sawBroadcast := false
	viewerConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for i := 0; i < 10; i++ {
		msgType, data, err := viewerConn.ReadMessage()
		if err != nil {
			break
		}
		if msgType != websocket.BinaryMessage || len(data) < 2 {
			continue
		}
		if err := viewerReplica.ReceiveSyncMessage(viewerReplicaState, data[1:]); err != nil {
			continue
		}
		if v, _, err := viewerReplica.Get(crdt.Root, "approved"); err == nil && v.Str == "from-writer" {
			sawBroadcast = true
			break
		}
	}
	if !sawBroadcast {
		t.Fatal("view-only client never received the server -> client broadcast of the writer's change")
	}

	authMu.Lock()
	calls := len(authCalls)
	authMu.Unlock()
	if calls == 0 {
		t.Fatal("expected the binary authorizer to have been consulted")
	}
	_ = viewerID
}
