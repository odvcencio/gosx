package docs

func Page() Node {
	return <div>
		<section id="hub-rooms">
			<h2>Hub Rooms</h2>
			<p>
				A hub is the fifth primitive in the GoSX platform model — alongside server,
				action, island, and engine. It is a long-lived server-side coordinator for
				realtime state: chat, presence, multiplayer, subscriptions, and fanout.
			</p>
			<p>
				Create a hub with <span class="inline-code">hub.New</span>. Each hub has a
				name and a set of event handlers registered via
				<span class="inline-code">hub.On</span>. The name appears in the client
				connection URL.
			</p>
			{CodeBlock("go", `import "github.com/odvcencio/gosx/hub"

// Declare the hub once at package level.
var collab = hub.New("collab")

func init() {
    // Handle a custom "cursor" event from any client.
    collab.On("cursor", func(ctx *hub.Context) error {
        // Broadcast cursor position to all other clients in the room.
        return ctx.BroadcastOthers("cursor", ctx.Payload)
    })

    // Handle client connect / disconnect lifecycle.
    collab.OnConnect(func(ctx *hub.Context) error {
        return ctx.Broadcast("presence", map[string]any{
            "event":    "join",
            "clientID": ctx.Client.ID,
        })
    })
}`)}
			<p>
				Mount the hub's HTTP handler at a URL. GoSX upgrades the connection to
				WebSocket automatically.
			</p>
			{CodeBlock("go", `// In main.go or your router setup
router.Handle("/ws/collab", collab.Handler())`)}
		</section>

		<section id="websocket-protocol">
			<h2>WebSocket Protocol</h2>
			<p>
				The hub wire protocol is a JSON envelope with an
				<span class="inline-code">event</span> string and an arbitrary
				<span class="inline-code">payload</span> object. Clients send and receive the
				same envelope shape, keeping the protocol easy to inspect in browser DevTools.
			</p>
			{CodeBlock("go", `// Server sends to a single client
ctx.Send("notification", map[string]any{
    "message": "Your document was saved.",
    "at":      time.Now().Unix(),
})

// Broadcast to every client in the room
ctx.Broadcast("presence", map[string]any{"event": "join", "clientID": ctx.Client.ID})

// Broadcast to everyone except the sender
ctx.BroadcastOthers("cursor", cursorPayload)

// Send to a specific client by ID
ctx.SendTo(targetClientID, "dm", payload)`)}
			<p>
				The hub runs entirely on the server. There is no generated client library:
				connect with any WebSocket client or the browser's native
				<span class="inline-code">WebSocket</span> API.
			</p>
			{CodeBlock("javascript", `const ws = new WebSocket("wss://example.com/ws/collab")

ws.onmessage = (e) => {
    const { event, payload } = JSON.parse(e.data)
    if (event === "cursor") updateCursor(payload)
}

function sendCursor(x, y) {
    ws.send(JSON.stringify({ event: "cursor", payload: { x, y } }))
}`)}
			<p>
				Shared state can be stored and read on the hub directly without a database
				for ephemeral room data such as cursor positions and presence lists:
			</p>
			{CodeBlock("go", `// Write
collab.SetState("count", activeCount)

// Read
count, _ := collab.State("count")`)}
		</section>

		<section id="crdt-documents">
			<h2>CRDT Documents</h2>
			<p>
				GoSX ships a first-party CRDT engine in
				<span class="inline-code">github.com/odvcencio/gosx/crdt</span>. A
				<span class="inline-code">crdt.Doc</span> is a conflict-free replicated
				document that can be read and mutated concurrently by multiple actors and
				will always converge to the same state regardless of operation order.
			</p>
			{CodeBlock("go", `import "github.com/odvcencio/gosx/crdt"

// Create a new document.
doc := crdt.NewDoc()

// Put a value into the root map.
crdt.Put(doc, crdt.Root, "title", crdt.Str("Untitled"))
crdt.Put(doc, crdt.Root, "counter", crdt.Int(0))
crdt.Put(doc, crdt.Root, "active", crdt.Bool(true))

// Commit the pending operations into a change.
change, err := crdt.Commit(doc)

// Read values back.
title, _ := crdt.Get(doc, crdt.Root, "title")  // crdt.Value
fmt.Println(title.String())                     // "Untitled"

counter, _ := crdt.Get(doc, crdt.Root, "counter")
fmt.Println(counter.Int())                      // 0`)}
			<p>
				Values are typed. The <span class="inline-code">crdt.Value</span> type carries
				a kind tag and exposes typed accessor methods:
			</p>
			{CodeBlock("go", `v.String()  // string value
v.Int()     // int64 value
v.Float()   // float64 value
v.Bool()    // bool value
v.ObjID()   // nested object ID`)}
			<p>
				Nested objects (sub-maps and lists) are created with
				<span class="inline-code">crdt.MakeMap</span> and
				<span class="inline-code">crdt.MakeList</span>:
			</p>
			{CodeBlock("go", `// Create a sub-map
prefs, _ := crdt.MakeMap(doc, crdt.Root, "prefs")
crdt.Put(doc, prefs, "theme", crdt.Str("dark"))

// Create a list
items, _ := crdt.MakeList(doc, crdt.Root, "items")
crdt.Append(doc, items, crdt.Str("first"))
crdt.Append(doc, items, crdt.Str("second"))

// Delete a key
crdt.Delete(doc, crdt.Root, "active")`)}
		</section>

		<section id="merge-sync">
			<h2>Merge &amp; Sync</h2>
			<p>
				Two documents merge without conflict by passing one document's changes into
				the other via <span class="inline-code">crdt.Merge</span>. The result is
				identical regardless of which side initiates the merge.
			</p>
			{CodeBlock("go", `// Alice and Bob start from the same base.
alice := crdt.NewDoc()
bob := crdt.NewDoc()

// Alice edits title.
crdt.Put(alice, crdt.Root, "title", crdt.Str("Alice's Doc"))
aliceChange, _ := crdt.Commit(alice)

// Bob edits counter concurrently.
crdt.Put(bob, crdt.Root, "counter", crdt.Int(42))
bobChange, _ := crdt.Commit(bob)

// Merge: both docs converge to the same state.
crdt.Merge(alice, bobChange)
crdt.Merge(bob, aliceChange)

// Both docs now have title="Alice's Doc" and counter=42.`)}
			<p>
				For network sync, the CRDT engine uses an efficient vector-clock protocol
				to exchange only the operations each peer has not yet seen:
			</p>
			{CodeBlock("go", `import "github.com/odvcencio/gosx/crdt/sync"

// Peer A generates a sync message describing its state.
msg, err := sync.GenerateSyncMessage(docA)

// Peer B receives the message and generates a response.
response, changes, err := sync.ReceiveSyncMessage(docB, msg)

// Apply changes to docB.
for _, c := range changes {
    crdt.Merge(docB, c)
}

// Peer A applies the response.
_, newChanges, err := sync.ReceiveSyncMessage(docA, response)
for _, c := range newChanges {
    crdt.Merge(docA, c)
}`)}
			<p>
				In a hub handler, run one sync round per WebSocket message to keep all
				connected peers converged:
			</p>
			{CodeBlock("go", `collab.On("sync", func(ctx *hub.Context) error {
    var msg []byte
    if err := json.Unmarshal(ctx.Payload, &msg); err != nil {
        return err
    }
    response, changes, err := sync.ReceiveSyncMessage(sharedDoc, msg)
    if err != nil {
        return err
    }
    for _, c := range changes {
        crdt.Merge(sharedDoc, c)
    }
    return ctx.BroadcastOthers("sync", response)
})`)}
		</section>

		<section id="patches-hooks">
			<h2>Patches &amp; Hooks</h2>
			<p>
				Every <span class="inline-code">crdt.Commit</span> or
				<span class="inline-code">crdt.Merge</span> produces a slice of
				<span class="inline-code">crdt.Patch</span> values describing exactly which
				paths changed. Register a hook to react to those patches and update UI or
				notify clients.
			</p>
			{CodeBlock("go", `// Register a change hook on the document.
doc.OnChange(func(patches []crdt.Patch) {
    for _, p := range patches {
        switch p.Action {
        case crdt.PatchPut:
            log.Printf("set %s = %v", p.Key, p.Value)
        case crdt.PatchDelete:
            log.Printf("deleted %s", p.Key)
        case crdt.PatchInsert:
            log.Printf("list insert at %s[%d]", p.ObjID, p.Index)
        }
    }
})`)}
			<p>
				In a collaborative editor, patches are the minimal diff to ship to clients.
				Convert them to JSON and broadcast over the hub instead of sending the full
				document on every change:
			</p>
			{CodeBlock("go", `doc.OnChange(func(patches []crdt.Patch) {
    collab.Broadcast("patch", patches)
})`)}
			{CodeBlock("javascript", `ws.onmessage = (e) => {
    const { event, payload } = JSON.parse(e.data)
    if (event === "patch") applyPatches(localDoc, payload)
}`)}
		</section>

		<section id="use-cases">
			<h2>Use Cases</h2>
			<div class="hubs-use-case-grid">
				<div class="hubs-use-case-card glass-panel">
					<h3>Collaborative Editing</h3>
					<p>
						Multiple users edit the same document simultaneously. Each client holds a
						local CRDT doc, commits to it on keypress, and syncs with the hub. Merges
						are automatic and conflict-free.
					</p>
				</div>
				<div class="hubs-use-case-card glass-panel">
					<h3>Shared Presence</h3>
					<p>
						Track cursors, selections, and active users. Store presence state on the
						hub, broadcast updates via <span class="inline-code">BroadcastOthers</span>,
						and clean up in the <span class="inline-code">OnDisconnect</span> handler.
					</p>
				</div>
				<div class="hubs-use-case-card glass-panel">
					<h3>Live Dashboards</h3>
					<p>
						Push metric updates from a background goroutine to all connected dashboard
						clients with a single <span class="inline-code">hub.Broadcast</span> call.
						No polling, no intermediate message queue.
					</p>
				</div>
				<div class="hubs-use-case-card glass-panel">
					<h3>Multiplayer State</h3>
					<p>
						Game or canvas sessions where clients agree on shared world state. Use
						CRDT docs for eventual-consistent properties and raw hub messages for
						low-latency ephemeral events.
					</p>
				</div>
				<div class="hubs-use-case-card glass-panel">
					<h3>Offline-First Apps</h3>
					<p>
						Clients edit their local CRDT doc while offline. On reconnect, a single
						sync round converges the local and server states with no manual conflict
						resolution.
					</p>
				</div>
				<div class="hubs-use-case-card glass-panel">
					<h3>Notification Fanout</h3>
					<p>
						Route targeted notifications — by user ID, room, or topic — without a
						separate pub/sub broker. Hub handlers are plain Go functions with full
						access to your application context.
					</p>
				</div>
			</div>
		</section>
	</div>
}
