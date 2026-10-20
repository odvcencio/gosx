# Replaceable binary hub snapshots

Use `Hub.BroadcastBinarySnapshot(epoch, baseline, delta)` for server-owned
state where the newest update replaces an older pending update. The ordinary
text queue, `BroadcastBinary`, and CRDT sync keep their existing FIFO behavior.
The snapshot lane is opt-in through this method; no hub configuration is needed.

```go
// Increment epoch whenever a new complete keyframe is encoded.
// Cache that frame and supply it on every offer for the epoch.
h.BroadcastBinarySnapshot(epoch, keyframe, nil)

// Encode each delta against keyframe, independently of previous deltas.
h.BroadcastBinarySnapshot(epoch, keyframe, delta)
```

Both frames must start with the application prefix `0x00`. Epoch must be
nonzero and increase for every new baseline. Empty baselines, zero epochs, and
non-application prefixes return zero without disconnecting clients. A nil or
empty delta offers only a baseline. The hub does not decode the application
protocol; applications must ensure each delta matches its supplied baseline.

Each client keeps at most two pending frames: its epoch baseline and the latest
delta. The first baseline offered for an epoch always precedes that epoch's
deltas. Further offers replace the pending delta and preserve that baseline.
A newer epoch replaces both old pending frames; an older epoch is ignored for
that client. Duplicate baseline-only offers do not resend a dispatched baseline
or discard a pending delta. Every offer includes the baseline so a new client
can receive a delta even if it joined after the keyframe broadcast.

The sole socket writer alternates snapshot frames with ready text, ordinary
binary, or timer work. One frame may already be in a socket write in addition
to the two pending frames. Bytes handed to a socket cannot be withdrawn; this
API bounds the hub backlog, not operating-system, proxy, or browser buffering.
A reconnect needs another application offer: snapshots are not latched.

The return value counts clients accepting the offer, including duplicate
offers for their current epoch. Stale offers are ignored per client, so a newer
client without that epoch can accept an offer that an older client ignores.
Applications should serialize their producers when they need one consistent
order across all clients. Within a client, concurrent offers take effect in
enqueue order and a stale epoch can never replace a newer pending epoch.

Payload ownership matches `BroadcastBinary`: the hub retains the provided
slices without copying them. Treat every offered frame as immutable. Create
new byte slices for subsequent encoded frames instead of reusing their storage.
A custom browser decoder is required; the stock browser runtime ignores hub
binary application frames.

`Hub.SnapshotStats()` and `Client.SnapshotStats()` report intentionally replaced
pending frames. Advancing an epoch can replace two frames; a baseline already
handed to the writer is never counted as replaced. Hub totals include clients
that have disconnected. These counters do not contribute to `DropStats` or
`SlowClient` eviction, which retain their existing full-queue loss semantics.

Existing observers also receive snapshot traffic through `Message`:

- `Snapshot=true` and `Binary=true` identify this lane.
- `Replaced=true` reports each superseded pending frame, including its bytes;
  `Dropped` and `Written` are false. `Count` coalesces identical events during
  fanout, and `Client` is nil for these fanout callbacks.
- A sampled offer has `QueueDepth` equal to the number of pending snapshot
  frames after the offer, from zero to two. Sampling has its own cadence and
  uses the existing telemetry sampling interval.
- `Written=true` reports a successfully written snapshot frame with its client,
  payload bytes, and `QueueDepth=-1`.

All callbacks run outside hub and client locks and retain the existing shutdown
ordering: admitted callbacks finish before the final `Closed` callback.
`Broadcast` reports accepted clients and zero full-queue drops for each valid
offer; intentional replacements are counted only by replacement observations.
