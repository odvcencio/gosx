package scene3d

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/crdt"
	crdtsync "m31labs.dev/gosx/crdt/sync"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/scene"
)

// peer is one connected client with its own document and sync state.
//
// A dedicated goroutine reads the socket and forwards frames over channels. A
// read deadline is NOT usable as a loop terminator here: gorilla/websocket
// stores the first read error, including a timeout, and every later read on the
// same connection returns that stored error. A drain loop that reads until it
// times out therefore closes its own read side, and the next frame the server
// sends is never seen.
type peer struct {
	conn      *websocket.Conn
	prefix    byte
	doc       *crdt.Doc
	bound     *Doc
	state     *crdtsync.State
	bootstrap []byte
	binary    chan []byte
	refusals  chan string
	errors    []string
}

// TestServerRefusesForbiddenObjectWrite is property 5. A client that may not
// write one object pushes a move of that object. The server scene must stay
// untouched, and the peer must keep working: its next allowed write must land.
func TestServerRefusesForbiddenObjectWrite(t *testing.T) {
	base := seedScene()
	serverBound, err := Bind(crdt.NewDoc(), "scene")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := serverBound.DiffScene(scene.SceneIR{}, base, "seed"); err != nil {
		t.Fatal(err)
	}

	h := hub.New("collab")
	// The guard reads immutable connection metadata, which the client cannot
	// change after the upgrade. "cube" is locked for every client.
	guard := func(client *hub.Client, target Target) bool {
		if target.ObjectID == "cube" {
			role, _ := client.Metadata("role")
			return role == "owner"
		}
		return true
	}
	if err := Serve(h, "room", serverBound, guard); err != nil {
		t.Fatal(err)
	}

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTPWithMetadata(w, r, hub.ConnectionMetadata{"role": r.URL.Query().Get("role")})
	}))
	defer httpServer.Close()

	client := dialPeer(t, httpServer.URL, "guest")
	defer client.conn.Close()

	// Baseline: the client sees the seeded scene.
	if got := objectByID(t, mustView(t, client.bound), "cube").X; got != 0 {
		t.Fatalf("bootstrap cube x = %v, want 0", got)
	}

	// The forbidden write.
	_, forbidden, err := client.bound.DiffScene(base, moveObject(base, "cube", 99), "guest moves the cube")
	if err != nil {
		t.Fatal(err)
	}
	// Force the Bloom false positive that made this test flaky. The client now
	// believes the server already holds the new change, so its first frame
	// advertises a head with no change and the server must ask for it. Forcing
	// the case turns a random flake into a covered path.
	client.state.PeerBloom = crdtsync.NewBloomFilterForHashes([][32]byte{[32]byte(forbidden)})
	if !pushSync(t, client, never) {
		t.Fatal("the server neither refused the forbidden write nor reported an error")
	}
	refusal := client.errors[0]
	if !strings.Contains(refusal, "cube") {
		t.Fatalf("the refusal does not name the object: %q", refusal)
	}

	// The server scene must be untouched.
	serverView := mustView(t, serverBound)
	if got := objectByID(t, serverView, "cube").X; got != 0 {
		t.Fatalf("server cube x = %v: a refused write reached the server scene", got)
	}
	if len(serverView.IR.Objects) != 2 {
		t.Fatalf("server object count = %d, want 2", len(serverView.IR.Objects))
	}

	// A refused change poisons every later local change, because every later
	// change names it as a dependency. Prove that hazard is real, so nobody
	// "fixes" Rebase away later.
	if _, _, err := client.bound.DiffScene(base, moveObject(base, "ball", 12), "guest moves the ball"); err != nil {
		t.Fatal(err)
	}
	client.errors = nil
	pushSync(t, client, never)
	for _, record := range mustView(t, serverBound).IR.Objects {
		if record.ID == "ball" && record.X == 12 {
			t.Fatal("a change that depends on a refused change reached the server scene")
		}
	}

	// The documented recovery: rebase onto the authoritative bootstrap and
	// replay only the allowed work. A reconnecting client receives exactly that
	// bootstrap frame.
	repaired := dialPeer(t, httpServer.URL, "guest")
	defer repaired.conn.Close()
	rebased, state, err := Rebase("scene", repaired.bootstrap, nil, "rebase")
	if err != nil {
		t.Fatal(err)
	}
	if got := objectByID(t, mustView(t, rebased), "cube").X; got != 0 {
		t.Fatalf("the rebased peer still shows the refused move: cube x = %v", got)
	}
	repaired.bound = rebased
	repaired.doc = rebased.Doc()
	repaired.state = state

	if _, _, err := repaired.bound.DiffScene(base, moveObject(base, "ball", 12), "rebased move"); err != nil {
		t.Fatal(err)
	}
	serverHasBallAt12 := func() bool {
		view, err := serverBound.View()
		if err != nil {
			return false
		}
		for _, record := range view.IR.Objects {
			if record.ID == "ball" && record.X == 12 {
				return true
			}
		}
		return false
	}
	if !pushSync(t, repaired, serverHasBallAt12) {
		t.Fatal("the rebased write never reached the server")
	}
	if len(repaired.errors) > 0 {
		t.Fatalf("the server refused the rebased write: %v", repaired.errors)
	}

	// The forbidden object is still untouched on the server.
	if got := objectByID(t, mustView(t, serverBound), "cube").X; got != 0 {
		t.Fatalf("server cube x = %v after the rebased write", got)
	}
}

// TestRebaseDropsTheRefusedChangeAndKeepsTheRest proves Rebase in isolation: the
// new document carries the authoritative state and the replayed commands, and
// nothing of the abandoned history.
func TestRebaseDropsTheRefusedChangeAndKeepsTheRest(t *testing.T) {
	base := seedScene()
	server, _ := newSeededPeer(t, base)
	client := forkPeer(t, server)

	if _, _, err := client.DiffScene(base, moveObject(base, "cube", 99), "refused move"); err != nil {
		t.Fatal(err)
	}
	if got := objectByID(t, mustView(t, client), "cube").X; got != 99 {
		t.Fatalf("client cube x = %v, want the local optimistic 99", got)
	}

	bootstrap, ok := server.Doc().GenerateSyncMessage(crdtsync.NewState())
	if !ok {
		t.Fatal("the server produced no bootstrap frame")
	}
	keep := []scene.Command{{
		Kind: scene.CommandSetTransform, ObjectID: "ball", Data: map[string]any{"x": 3.0},
	}}
	rebased, state, err := Rebase("scene", bootstrap, keep, "rebase")
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("Rebase returned no sync state")
	}
	view := mustView(t, rebased)
	if got := objectByID(t, view, "cube").X; got != 0 {
		t.Fatalf("rebased cube x = %v, want the server value 0", got)
	}
	if len(view.IR.Objects) != 2 {
		t.Fatalf("rebased object count = %d, want 2", len(view.IR.Objects))
	}
	if _, ok := view.Transforms["ball"]; !ok {
		t.Fatal("Rebase dropped the replayed command")
	}

	// The server accepts the rebased history, because it depends only on
	// changes the server already holds.
	before := canonicalVisible(t, server)
	if err := server.Doc().Merge(rebased.Doc()); err != nil {
		t.Fatal(err)
	}
	after := mustView(t, server)
	if got := objectByID(t, after, "cube").X; got != 0 {
		t.Fatalf("the rebased history moved the server cube to %v", got)
	}
	if _, ok := after.Transforms["ball"]; !ok {
		t.Fatalf("the replayed command never reached the server\nbefore %s", before)
	}
}

// TestGuardRefusesTheIndexEntryToo proves a client cannot grow the object index
// for an object it may not create. Without that check a refused client could
// still make the index carry a name the scene never gets.
func TestGuardRefusesTheIndexEntryToo(t *testing.T) {
	serverBound, err := Bind(crdt.NewDoc(), "scene")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := serverBound.DiffScene(scene.SceneIR{}, seedScene(), "seed"); err != nil {
		t.Fatal(err)
	}
	client := forkPeer(t, serverBound)
	if _, err := client.Apply([]scene.Command{
		scene.CreateObjectCommand(scene.ObjectIR{ID: "intruder", Kind: "box"}),
	}, "client adds"); err != nil {
		t.Fatal(err)
	}

	var refusedTargets []Target
	gate := ChangeGate("room", serverBound, func(_ *hub.Client, target Target) bool {
		if target.ObjectID == "intruder" {
			refusedTargets = append(refusedTargets, target)
			return false
		}
		return true
	}, nil)

	changes := changesOf(t, client.Doc())
	if err := gate(nil, "room", changes); err == nil {
		t.Fatal("the gate accepted a forbidden create")
	}
	if len(refusedTargets) == 0 {
		t.Fatal("the guard never saw the forbidden object")
	}
	sawIndex := false
	for _, target := range refusedTargets {
		if target.Index {
			sawIndex = true
		}
	}
	if !sawIndex {
		t.Fatalf("the guard never saw the index entry: %v", refusedTargets)
	}
}

// TestGateFailsClosedOnAnOffSchemaKey proves a write inside the namespace that
// the schema does not describe is refused. A gate that ignored an unknown key
// would let a client write past the schema.
func TestGateFailsClosedOnAnOffSchemaKey(t *testing.T) {
	serverBound, err := Bind(crdt.NewDoc(), "scene")
	if err != nil {
		t.Fatal(err)
	}
	client := crdt.NewDoc()
	if err := client.Put(crdt.Root, "scene/sneaky", crdt.StringValue("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Commit("off schema"); err != nil {
		t.Fatal(err)
	}
	gate := ChangeGate("room", serverBound, func(_ *hub.Client, target Target) bool {
		// A well-formed target is allowed; the off-schema target has no object,
		// no slot, and no index flag, so this guard refuses it.
		return target.ObjectID != "" || target.Slot != "" || target.Index
	}, nil)
	if err := gate(nil, "room", changesOf(t, client)); err == nil {
		t.Fatal("the gate accepted an off-schema key inside the namespace")
	}
}

// TestGatePassesForeignNamespaceToNext proves a document shared with another
// feature still works. The gate must not claim a key it does not own.
func TestGatePassesForeignNamespaceToNext(t *testing.T) {
	serverBound, err := Bind(crdt.NewDoc(), "scene")
	if err != nil {
		t.Fatal(err)
	}
	client := crdt.NewDoc()
	if err := client.Put(crdt.Root, "chat/title", crdt.StringValue("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Commit("chat"); err != nil {
		t.Fatal(err)
	}

	nextCalled := 0
	gate := ChangeGate("room", serverBound, func(*hub.Client, Target) bool {
		t.Error("the scene guard saw a foreign key")
		return false
	}, func(_ *hub.Client, _ string, changes []crdt.Change) error {
		nextCalled++
		if len(changes) != 1 || len(changes[0].Ops) != 1 {
			t.Errorf("next received %d changes", len(changes))
		}
		return nil
	})
	if err := gate(nil, "room", changesOf(t, client)); err != nil {
		t.Fatalf("the gate refused a foreign key: %v", err)
	}
	if nextCalled != 1 {
		t.Fatalf("next authorizer calls = %d, want 1", nextCalled)
	}
}

// TestGateIgnoresAnotherDocument proves the gate only claims the document it was
// built for, so one hub can carry several documents.
func TestGateIgnoresAnotherDocument(t *testing.T) {
	serverBound, err := Bind(crdt.NewDoc(), "scene")
	if err != nil {
		t.Fatal(err)
	}
	gate := ChangeGate("room", serverBound, func(*hub.Client, Target) bool {
		t.Error("the scene guard ran for another document")
		return false
	}, nil)
	if err := gate(nil, "other-room", nil); err != nil {
		t.Fatalf("the gate refused another document: %v", err)
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func mustView(t *testing.T, d *Doc) View {
	t.Helper()
	view, err := d.View()
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	return view
}

// changesOf returns every change a document holds, decoded the way the hub
// decodes an inbound sync frame.
func changesOf(t *testing.T, doc *crdt.Doc) []crdt.Change {
	t.Helper()
	state := crdtsync.NewState()
	msg, ok := doc.GenerateSyncMessage(state)
	if !ok {
		t.Fatal("the document produced no sync message")
	}
	decoded, err := crdtsync.DecodeMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	changes := make([]crdt.Change, 0, len(decoded.Changes))
	for _, chunk := range decoded.Changes {
		change, err := crdt.DecodeChangeChunk(chunk)
		if err != nil {
			t.Fatal(err)
		}
		changes = append(changes, change)
	}
	if len(changes) == 0 {
		t.Fatal("the sync message carried no change")
	}
	return changes
}

// dialPeer connects one client, starts its reader, takes the bootstrap frame,
// and binds the received document.
func dialPeer(t *testing.T, serverURL, role string) *peer {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "?role=" + role
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	client := &peer{
		conn:     conn,
		doc:      crdt.NewDoc(),
		state:    crdtsync.NewState(),
		binary:   make(chan []byte, 64),
		refusals: make(chan string, 64),
	}
	client.bound, err = Bind(client.doc, "scene")
	if err != nil {
		t.Fatal(err)
	}
	go client.readLoop()

	select {
	case frame := <-client.binary:
		if len(frame) < 2 {
			t.Fatalf("short sync frame: %d bytes", len(frame))
		}
		client.prefix = frame[0]
		client.bootstrap = append([]byte(nil), frame[1:]...)
		if err := client.doc.ReceiveSyncMessage(client.state, frame[1:]); err != nil {
			t.Fatalf("bootstrap sync: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no bootstrap sync frame")
	}
	return client
}

// readLoop forwards every frame until the connection closes. It never touches
// the document, so the test goroutine stays the only writer.
func (p *peer) readLoop() {
	for {
		msgType, data, err := p.conn.ReadMessage()
		if err != nil {
			close(p.binary)
			return
		}
		switch msgType {
		case websocket.BinaryMessage:
			p.binary <- append([]byte(nil), data...)
		case websocket.TextMessage:
			var message hub.Message
			if err := json.Unmarshal(data, &message); err != nil || message.Event != "__crdt_error" {
				continue
			}
			var payload struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(message.Data, &payload); err == nil {
				p.refusals <- payload.Error
			}
		}
	}
}

// pushSync sends the client's pending changes to the server and drives the
// exchange until done reports true, a refusal arrives, or the round budget runs
// out. It reports whether done was reached.
//
// One round is not enough, and the reason is not a race in this test. A
// Bloom-filter false positive can make the client's first frame advertise a new
// head WITHOUT carrying the change. The server then replies with a Need and the
// client must send again. The hub's own sync test documents the same path; see
// driveClientSyncUntil in hub/sync_test.go.
func pushSync(t *testing.T, client *peer, done func() bool) bool {
	t.Helper()
	for round := 0; round < 8; round++ {
		msg, ok := client.doc.GenerateSyncMessage(client.state)
		if !ok && round == 0 {
			t.Fatal("the client produced no sync message")
		}
		if ok {
			if err := client.conn.WriteMessage(websocket.BinaryMessage, append([]byte{client.prefix}, msg...)); err != nil {
				t.Fatalf("write sync (round %d): %v", round+1, err)
			}
		}
		drain(t, client, 250*time.Millisecond)
		if len(client.errors) > 0 || done() {
			return true
		}
	}
	return false
}

// never is the done predicate for a push that must NOT converge.
func never() bool { return false }

// drain applies whatever the server has already sent, then waits up to window
// for one more frame. It collects refusals without consuming the read side.
func drain(t *testing.T, client *peer, window time.Duration) {
	t.Helper()
	timer := time.NewTimer(window)
	defer timer.Stop()
	for {
		select {
		case refusal := <-client.refusals:
			client.errors = append(client.errors, refusal)
			return
		case frame, open := <-client.binary:
			if !open {
				return
			}
			if len(frame) >= 2 && frame[0] == client.prefix {
				if err := client.doc.ReceiveSyncMessage(client.state, frame[1:]); err != nil {
					t.Fatalf("apply server sync: %v", err)
				}
			}
		case <-timer.C:
			return
		}
	}
}
