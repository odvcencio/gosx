# Hub sync delivery and slow clients

Each WebSocket connection has bounded queues of 256 text frames and 256 binary
frames. These are frame-count bounds, not byte budgets. Text and application
binary fanout remain best effort: a full queue drops the new message and updates
`Client.DropStats()` without disconnecting the client.

CRDT sync needs a different overflow policy. Generating a sync frame advances
optimistic per-peer state, so silently dropping that frame could leave an
unchanged document permanently behind. If a generated CRDT frame cannot enter
the binary queue, the hub marks that connection terminal before any other
producer can generate or enqueue another frame. It then closes the socket;
normal read-pump cleanup removes the client and presence and calls the leave
handler. The close is immediate, without waiting to write a close control frame
through a potentially blocked socket.

A reconnect creates fresh per-document sync states and runs normal bootstrap
with the current read authorizer. It can recover both a rejected final update
and previously queued-but-unwritten updates, without a new document edit or
inbound peer message. Unauthorized documents are not bootstrapped. This does not
change the wire format or promise uninterrupted delivery: the peer must
reconnect, remain authorized, and have enough transport capacity to sync. A
client that stays slow may be disconnected again. The hub does not add retries,
an unbounded repair queue, or a reconnect scheduler.

Successful enqueue is not a socket-write acknowledgment or peer acknowledgment.
The single write pump owns data writes and their deadlines; a write failure
closes the socket and likewise requires fresh reconnect/resync. It does not hold
the queue mutex while writing, so a blocked socket does not stall document
fanout to other clients. Inbound authorization and change authorization retain
their existing behavior.

`sync_backpressure_test.go` covers the production-size queue, terminal marking
under concurrent producers, authorization, healthy peers, duplicate sync,
simultaneous receives/broadcasts, and a real WebSocket reconnect over a
deterministically blocked transport. Run the focused checks with:

```sh
go test -race ./hub -run '^(TestSyncBackpressure|TestConcurrentSync|TestNonSyncBackpressure)' -count=10
```
