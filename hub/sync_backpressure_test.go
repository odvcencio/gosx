package hub

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/crdt"
	crdtsync "m31labs.dev/gosx/crdt/sync"
)

func seededSyncDoc(t *testing.T, value string) *crdt.Doc {
	t.Helper()
	doc := crdt.NewDoc()
	if err := doc.Put(crdt.Root, "value", crdt.StringValue(value)); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Commit("seed"); err != nil {
		t.Fatal(err)
	}
	return doc
}

func fullSyncClient(h *Hub, id string) *Client {
	client := &Client{ID: id, Hub: h, send: make(chan []byte, 256), binarySend: make(chan []byte, 256), syncStates: newPeerSyncState()}
	for i := 0; i < cap(client.binarySend); i++ {
		client.binarySend <- []byte{0}
	}
	return client
}

func syncClientClosed(client *Client) bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.closed
}

// The audit observation asserted that dropping a generated message poisoned
// the peer state and an unchanged retry produced nothing. The desired contract
// is instead explicit: a dropped sync frame makes this connection terminal;
// recovery uses a new connection's fresh sync states, not the poisoned state.
func TestSyncBackpressureRequiresReconnectWithoutNewInput(t *testing.T) {
	h := New("sync-backpressure")
	h.SyncDoc("document", seededSyncDoc(t, "final"))
	client := fullSyncClient(h, "slow")
	binding := h.syncDocs[h.syncDocName["document"]]
	h.queueSyncMessage(client, binding)
	if got := client.DropStats().Binary; got != 1 {
		t.Fatalf("binary drops = %d, want 1", got)
	}
	if !syncClientClosed(client) {
		t.Fatal("lost final sync message left connection live with no guaranteed repair")
	}
	for len(client.binarySend) > 0 {
		<-client.binarySend
	}
	h.queueSyncMessage(client, binding)
	if len(client.binarySend) != 0 || client.DropStats().Binary != 1 {
		t.Fatal("terminal connection resumed queueing or counted another drop")
	}
}

func TestSyncBackpressurePreservesReadAuthorizationAndHealthyPeer(t *testing.T) {
	h := New("sync-backpressure-auth")
	h.SyncDoc("private", seededSyncDoc(t, "private"))
	h.SyncDoc("public", seededSyncDoc(t, "public"))
	h.SetBinaryReadAuthorizer(func(client *Client, name string) bool { return name == "public" || client.ID == "healthy" })
	slow := fullSyncClient(h, "slow")
	healthy := &Client{ID: "healthy", Hub: h, binarySend: make(chan []byte, 256), syncStates: newPeerSyncState()}
	h.clients[slow.ID], h.clients[healthy.ID] = slow, healthy
	h.broadcastSyncDoc(h.syncDocName["private"])
	if syncClientClosed(slow) || slow.DropStats().Total() != 0 {
		t.Fatal("unauthorized document caused a send or disconnect")
	}
	h.broadcastSyncDoc(h.syncDocName["public"])
	if !syncClientClosed(slow) || syncClientClosed(healthy) {
		t.Fatal("backpressure must terminate only the saturated authorized peer")
	}
	if len(healthy.binarySend) != 2 {
		t.Fatalf("healthy peer queued %d docs, want 2", len(healthy.binarySend))
	}
}

func TestConcurrentSyncBroadcastQueuesUnchangedDocumentOnce(t *testing.T) {
	h := New("concurrent-sync")
	h.SyncDoc("document", seededSyncDoc(t, "final"))
	client := &Client{Hub: h, binarySend: make(chan []byte, 256), syncStates: newPeerSyncState()}
	binding := h.syncDocs[h.syncDocName["document"]]
	var workers sync.WaitGroup
	for i := 0; i < 32; i++ {
		workers.Go(func() { h.queueSyncMessage(client, binding) })
	}
	workers.Wait()
	if got := len(client.binarySend); got != 1 {
		t.Fatalf("queued %d copies of unchanged document, want 1", got)
	}
}

func TestSyncBackpressureTerminatesBeforeConcurrentProducerReuse(t *testing.T) {
	h := New("terminal-sync")
	h.SyncDoc("one", seededSyncDoc(t, "one"))
	h.SyncDoc("two", seededSyncDoc(t, "two"))
	client := fullSyncClient(h, "slow")
	binding := h.syncDocs[h.syncDocName["one"]]
	var workers sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		workers.Go(func() { <-start; h.queueSyncMessage(client, binding) })
	}
	close(start)
	workers.Wait()
	if !syncClientClosed(client) || client.DropStats().Binary != 1 {
		t.Fatalf("failed enqueue not terminal: closed=%v drops=%+v", syncClientClosed(client), client.DropStats())
	}
	for len(client.binarySend) > 0 {
		<-client.binarySend
	}
	h.queueSyncMessage(client, h.syncDocs[h.syncDocName["two"]])
	if len(client.binarySend) != 0 || client.syncStates.docState(h.syncDocName["two"]).Initialized {
		t.Fatal("another producer generated or queued sync on the terminal connection")
	}
}

func TestNonSyncBackpressureRemainsBestEffort(t *testing.T) {
	h := New("best-effort")
	client := fullSyncClient(h, "slow")
	h.clients[client.ID] = client
	for i := 0; i < cap(client.send); i++ {
		client.send <- []byte("filler")
	}
	h.Broadcast("tick", 1)
	if queued := h.BroadcastBinary([]byte{0}); queued != 0 {
		t.Fatalf("queued=%d, want 0", queued)
	}
	if syncClientClosed(client) || client.DropStats().Text != 1 || client.DropStats().Binary != 1 {
		t.Fatalf("best-effort traffic changed terminal policy: closed=%v drops=%+v", syncClientClosed(client), client.DropStats())
	}
}

func TestConcurrentSyncReceiveAndBroadcastConverge(t *testing.T) {
	h := New("sync-receive-race")
	doc := seededSyncDoc(t, "server")
	h.SyncDoc("document", doc)
	prefix := h.syncDocName["document"]
	peer := crdt.NewDoc()
	if err := peer.Put(crdt.Root, "remote", crdt.StringValue("client")); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.Commit("client update"); err != nil {
		t.Fatal(err)
	}
	peerState := crdtsync.NewState()
	message, ok := peer.GenerateSyncMessage(peerState)
	if !ok {
		t.Fatal("missing peer frame")
	}
	frame := append([]byte{prefix}, message...)
	client := &Client{ID: "peer", Hub: h, send: make(chan []byte, 256), binarySend: make(chan []byte, 256), syncStates: newPeerSyncState()}
	h.clients[client.ID] = client
	var workers sync.WaitGroup
	for i := 0; i < 32; i++ {
		workers.Go(func() { h.broadcastSyncDoc(prefix) })
		workers.Go(func() { h.handleBinaryMessage(client, frame) })
	}
	workers.Wait()
	if syncClientClosed(client) || len(client.send) != 0 {
		t.Fatal("concurrent valid sync closed/rejected peer")
	}
	for len(client.binarySend) > 0 {
		response := <-client.binarySend
		if err := peer.ReceiveSyncMessage(peerState, response[1:]); err != nil {
			t.Fatal(err)
		}
	}
	for key, want := range map[string]string{"value": "server", "remote": "client"} {
		for _, gotDoc := range []*crdt.Doc{doc, peer} {
			value, _, err := gotDoc.Get(crdt.Root, crdt.Prop(key))
			if err != nil || value.Str != want {
				t.Fatalf("%s=%q, want %q (err %v)", key, value.Str, want, err)
			}
		}
	}
}

// blockedSyncConn is a real WebSocket transport with a deterministic slow
// socket at the net.Conn boundary. No timing-dependent OS buffer saturation or
// huge allocations are needed. Close interrupts the write, as a socket does.
type blockedSyncConn struct {
	net.Conn
	block     atomic.Bool
	entered   chan struct{}
	closed    chan struct{}
	enterOnce sync.Once
	closeOnce sync.Once
}

func (c *blockedSyncConn) Write(data []byte) (int, error) {
	if c.block.Load() {
		c.enterOnce.Do(func() { close(c.entered) })
		<-c.closed
		return 0, net.ErrClosed
	}
	return c.Conn.Write(data)
}

func (c *blockedSyncConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

type blockedSyncListener struct {
	net.Listener
	accepted chan *blockedSyncConn
}

func (l *blockedSyncListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	wrapped := &blockedSyncConn{Conn: conn, entered: make(chan struct{}), closed: make(chan struct{})}
	l.accepted <- wrapped
	return wrapped, nil
}

func TestSyncBackpressureRealSocketReconnectsUnchangedDocuments(t *testing.T) {
	h := New("sync-socket")
	docs := map[string]*crdt.Doc{"one": seededSyncDoc(t, "one-before"), "two": seededSyncDoc(t, "two-final")}
	for name, doc := range docs {
		h.SyncDoc(name, doc)
	}
	h.SyncDoc("private", seededSyncDoc(t, "secret"))
	var readChecks sync.Map
	h.SetBinaryReadAuthorizer(func(client *Client, name string) bool {
		readChecks.Store(client.ID+"/"+name, true)
		return name != "private"
	})
	left := make(chan string, 2)
	h.On("leave", func(ctx *Context) { left <- ctx.Client.ID })
	server := httptest.NewUnstartedServer(h)
	listener := &blockedSyncListener{Listener: server.Listener, accepted: make(chan *blockedSyncConn, 4)}
	server.Listener = listener
	server.Start()
	defer server.Close()

	dial := func() (*websocket.Conn, *blockedSyncConn) {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), http.Header{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		transport := <-listener.accepted
		t.Cleanup(func() { transport.Close() })
		return conn, transport
	}
	readDocs := func(conn *websocket.Conn) (map[byte]*crdt.Doc, string) {
		t.Helper()
		result := make(map[byte]*crdt.Doc)
		if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		clientID := ""
		for len(result) < len(docs) || clientID == "" {
			kind, frame, err := conn.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			if kind != websocket.BinaryMessage {
				var welcome Message
				if err := json.Unmarshal(frame, &welcome); err != nil {
					t.Fatal(err)
				}
				if welcome.Event == "__welcome" {
					var identity struct {
						ClientID string `json:"clientId"`
					}
					if err := json.Unmarshal(welcome.Data, &identity); err != nil {
						t.Fatal(err)
					}
					clientID = identity.ClientID
				}
				continue
			}
			if len(frame) < 2 {
				t.Fatalf("short sync frame: %v", frame)
			}
			if frame[0] == h.syncDocName["private"] {
				t.Fatal("bootstrap exposed unauthorized document")
			}
			doc := crdt.NewDoc()
			if err := doc.ReceiveSyncMessage(crdtsync.NewState(), frame[1:]); err != nil {
				t.Fatal(err)
			}
			result[frame[0]] = doc
		}
		return result, clientID
	}
	conn, transport := dial()
	_, clientID := readDocs(conn)
	h.mu.RLock()
	client := h.clients[clientID]
	h.mu.RUnlock()
	if client == nil {
		t.Fatal("accepted client missing")
	}
	transport.block.Store(true)
	// This update is successfully queued but never written to the peer.
	// Reconnect must recover it too, not only the later enqueue failure.
	if err := docs["two"].Put(crdt.Root, "value", crdt.StringValue("two-queued-unwritten")); err != nil {
		t.Fatal(err)
	}
	if _, err := docs["two"].Commit("queued update"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-transport.entered:
	case <-time.After(time.Second):
		t.Fatal("socket writer did not enter slow transport")
	}
	if got := client.DropStats().Total(); got != 0 {
		t.Fatalf("accepted-but-unwritten update recorded %d drops", got)
	}
	// Direct fill only models competing queued frames; the final document
	// mutation below uses the real OnChange -> fanout -> queue path.
	for i := 0; i < cap(client.binarySend); i++ {
		client.binarySend <- []byte{0}
	}
	finished := make(chan error, 1)
	go func() {
		if err := docs["one"].Put(crdt.Root, "value", crdt.StringValue("one-final")); err != nil {
			finished <- err
			return
		}
		_, err := docs["one"].Commit("final update")
		finished <- err
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		transport.Close()
		<-finished
		t.Fatal("one blocked socket stalled document commit/fanout")
	}
	select {
	case <-transport.closed:
	case <-time.After(time.Second):
		t.Fatal("lost final update did not close slow socket")
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("slow socket still readable after terminal backpressure")
	}
	select {
	case id := <-left:
		if id != client.ID {
			t.Fatalf("removed client %q, want %q", id, client.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("backpressure did not run leave cleanup")
	}
	if h.ClientCount() != 0 || h.Presence().Count() != 0 {
		t.Fatal("backpressure leaked client or presence")
	}
	// No new edit, peer Need, or input arrives between the dropped final
	// update and this fresh connection's complete bootstrap.
	reconnected, _ := dial()
	received, nextID := readDocs(reconnected)
	h.mu.RLock()
	nextClient := h.clients[nextID]
	h.mu.RUnlock()
	if nextClient == nil || nextID == client.ID || nextClient.syncStates == client.syncStates {
		t.Fatal("reconnect reused the terminal peer's identity or sync states")
	}
	for _, candidate := range []*Client{client, nextClient} {
		if _, checked := readChecks.Load(candidate.ID + "/private"); !checked {
			t.Fatal("connection did not recheck read authorization")
		}
		candidate.syncStates.mu.Lock()
		_, privateState := candidate.syncStates.state[h.syncDocName["private"]]
		candidate.syncStates.mu.Unlock()
		if privateState {
			t.Fatal("unauthorized document acquired outbound sync state")
		}
	}
	for name, want := range map[string]string{"one": "one-final", "two": "two-queued-unwritten"} {
		doc := received[h.syncDocName[name]]
		if doc == nil {
			t.Fatalf("missing document %q", name)
		}
		value, _, err := doc.Get(crdt.Root, "value")
		if err != nil || value.Str != want {
			t.Fatalf("%s=%q, want %q (err %v)", name, value.Str, want, err)
		}
	}
	if got := client.DropStats().Binary; got != 1 {
		t.Fatalf("dropped %d frames, want 1", got)
	}
}
