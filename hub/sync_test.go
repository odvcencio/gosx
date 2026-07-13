package hub

import (
	"encoding/json"
	"fmt"
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

// driveClientSyncUntil completes the request/need exchange after a client has
// sent its first sync frame. A Bloom-filter false positive may cause that first
// frame to advertise a new head without carrying its change; in that case the
// server replies with Need and the client must send another round. Each server
// response is also an observable barrier for the asynchronous websocket pump.
func driveClientSyncUntil(
	t *testing.T,
	conn *websocket.Conn,
	prefix byte,
	clientDoc *crdt.Doc,
	clientState *crdtsync.State,
	serverDoc *crdt.Doc,
	prop crdt.Prop,
	want string,
) int {
	t.Helper()

	const maxRounds = 8
	lastObserved := "no server response"
	for round := 0; round < maxRounds; round++ {
		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("set sync response deadline: %v", err)
		}
		msgType, response, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read sync response (round %d): %v", round+1, err)
		}
		if msgType != websocket.BinaryMessage || len(response) < 2 || response[0] != prefix {
			t.Fatalf("expected prefixed binary sync response for doc %d, got type=%d payload=%v", prefix, msgType, response)
		}
		if err := clientDoc.ReceiveSyncMessage(clientState, response[1:]); err != nil {
			t.Fatalf("apply sync response (round %d): %v", round+1, err)
		}

		value, _, err := serverDoc.Get(crdt.Root, prop)
		if err == nil && value.Str == want {
			return round + 1
		}
		if err != nil {
			lastObserved = err.Error()
		} else {
			lastObserved = fmt.Sprintf("got %q", value.Str)
		}

		reply, ok := clientDoc.GenerateSyncMessage(clientState)
		if !ok {
			t.Fatalf("server did not converge %s=%q after round %d (%s), but client had no sync reply", prop, want, round+1, lastObserved)
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, append([]byte{prefix}, reply...)); err != nil {
			t.Fatalf("write sync reply (round %d): %v", round+1, err)
		}
	}

	t.Fatalf("server did not converge %s=%q after %d sync rounds: %s", prop, want, maxRounds, lastObserved)
	return 0
}

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

	if err := clientDoc.Put(crdt.Root, "title", crdt.StringValue("client")); err != nil {
		t.Fatalf("client title put: %v", err)
	}
	clientChange, err := clientDoc.Commit("client update")
	if err != nil {
		t.Fatalf("commit client update: %v", err)
	}
	// Deterministically model a Bloom false positive for the client's new
	// change. The first frame will advertise the new head without carrying the
	// change, so the server must request it and the client must complete a
	// second sync round. This is the formerly flaky path made reproducible.
	clientState.PeerBloom = crdtsync.NewBloomFilterForHashes([][32]byte{[32]byte(clientChange)})
	clientMsg, ok := clientDoc.GenerateSyncMessage(clientState)
	if !ok {
		t.Fatal("expected client sync message after local change")
	}

	if err := conn.WriteMessage(websocket.BinaryMessage, append([]byte{prefix}, clientMsg...)); err != nil {
		t.Fatalf("write client sync message: %v", err)
	}

	if rounds := driveClientSyncUntil(t, conn, prefix, clientDoc, clientState, doc, "title", "client"); rounds < 2 {
		t.Fatalf("forced Bloom false positive converged in %d round; want at least 2", rounds)
	}

	value, _, err = doc.Get(crdt.Root, "title")
	if err != nil {
		t.Fatalf("get server title: %v", err)
	}
	if value.Str != "client" {
		t.Fatalf("expected server title client, got %q", value.Str)
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
	sendMutation := func(conn *websocket.Conn, field, value string) (*crdt.Doc, *crdtsync.State) {
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
		return clientDoc, clientState
	}

	// Unauthorized (viewer) client's inbound sync is dropped: the server
	// document never gets "poisoned".
	sendMutation(viewerConn, "poisoned", "from-viewer")

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
	if _, _, err := doc.Get(crdt.Root, "poisoned"); err == nil {
		t.Fatal("unauthorized client's inbound sync frame was applied to the server document")
	}

	// Authorized (writer) client's inbound sync IS applied.
	writerDoc, writerState := sendMutation(writerConn, "approved", "from-writer")
	driveClientSyncUntil(t, writerConn, prefix, writerDoc, writerState, doc, "approved", "from-writer")
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

func TestHubBinaryChangeAuthorizerRejectsActorSubstitutionBeforeMerge(t *testing.T) {
	h := New("collab-change-auth")
	serverDoc := crdt.NewDoc()
	if err := serverDoc.Put(crdt.Root, "title", crdt.StringValue("server")); err != nil {
		t.Fatal(err)
	}
	if _, err := serverDoc.Commit("seed"); err != nil {
		t.Fatal(err)
	}
	h.SyncDoc("room-actor", serverDoc)
	prefix := h.syncDocName["room-actor"]

	clientDoc := crdt.NewDoc()
	clientState := crdtsync.NewState()
	bootstrapState := crdtsync.NewState()
	bootstrap, ok := serverDoc.GenerateSyncMessage(bootstrapState)
	if !ok {
		t.Fatal("expected bootstrap sync message")
	}
	if err := clientDoc.ReceiveSyncMessage(clientState, bootstrap); err != nil {
		t.Fatal(err)
	}
	if err := clientDoc.Put(crdt.Root, "title", crdt.StringValue("client")); err != nil {
		t.Fatal(err)
	}
	if _, err := clientDoc.Commit("client edit"); err != nil {
		t.Fatal(err)
	}
	frame, ok := clientDoc.GenerateSyncMessage(clientState)
	if !ok {
		t.Fatal("expected client sync message")
	}

	client := &Client{
		ID:         "writer",
		Hub:        h,
		send:       make(chan []byte, 2),
		binarySend: make(chan []byte, 2),
		syncStates: newPeerSyncState(),
	}
	h.SetBinaryChangeAuthorizer(func(_ *Client, docName string, changes []crdt.Change) error {
		if docName != "room-actor" || len(changes) == 0 {
			return fmt.Errorf("missing authorized changes")
		}
		for _, change := range changes {
			if change.ActorID != "bound-actor" {
				return fmt.Errorf("actor %q is not bound to connection", change.ActorID)
			}
		}
		return nil
	})
	h.handleBinaryMessage(client, append([]byte{prefix}, frame...))
	value, _, err := serverDoc.Get(crdt.Root, "title")
	if err != nil || value.Str != "server" {
		t.Fatalf("rejected frame mutated document: value=%q err=%v", value.Str, err)
	}
	select {
	case payload := <-client.send:
		if !strings.Contains(string(payload), "not bound to connection") {
			t.Fatalf("unexpected rejection payload %s", payload)
		}
	default:
		t.Fatal("expected CRDT authorization error")
	}

	boundActor := clientDoc.ActorID().String()
	h.SetBinaryChangeAuthorizer(func(_ *Client, _ string, changes []crdt.Change) error {
		for _, change := range changes {
			if change.ActorID != boundActor {
				return fmt.Errorf("actor mismatch")
			}
		}
		return nil
	})
	h.handleBinaryMessage(client, append([]byte{prefix}, frame...))
	value, _, err = serverDoc.Get(crdt.Root, "title")
	if err != nil || value.Str != "client" {
		t.Fatalf("authorized frame not merged: value=%q err=%v", value.Str, err)
	}
}
