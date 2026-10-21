# netplay: rollback netcode for deterministic simulations

`m31labs.dev/gosx/netplay` runs a two-player deterministic simulation over an
unreliable network. Each peer simulates every frame at once, guesses the other
player's input, and rewinds and replays when the guess was wrong. The package
uses only the standard library and does no I/O: you supply a `Transport` and a
clock.

## What you write

A game implements `Game[I]`, where `I` is one player's input for one frame:

```go
type Game[I any] interface {
    Step(inputs []I)            // advance exactly one frame; inputs indexed by player
    Save(dst []byte) []byte     // full state, deterministic encoding
    Load(src []byte) error
    Events(dst []Event) []Event // every event emitted by the last Step, in order
}
```

Rules the engine relies on:

- An input is the absolute held state for one frame. Derive press and release
  edges inside `Step` from the previous frame's held state. Then repeating the
  last input as a prediction never fires a press twice.
- `Save` holds simulation state only: no session, connection or UI state. The
  checksum is `netplay.Checksum(Save bytes)`.
- `Events` returns the whole list for the frame. A game that keeps one "last
  event" loses same-frame trades.
- In steady state `Step`, `Save`, `Load` and `Events` must not allocate.
- The game must produce the same bytes on every platform. Use integer
  fixed-point, or the `det` toolkit for floating point.

You also give a `Codec[I]` that encodes an input to a fixed size (1 to 64 bytes).

## One peer

```go
s, err := netplay.NewSession(netplay.Config[Pad]{
    Game: game, Codec: padCodec{}, Transport: tr,
    LocalPlayer: 0,                    // 0 or 1
    Identity:    sim.SimulationIdentity,
    InputDelay:  netplay.AutoDelay,    // or 0 to 3
    Relay:       true,                 // a relay arbitrates desyncs
})

// every 1/60 s, with a monotonic clock:
switch s.Advance(now, localInput) {
case netplay.OutcomeAdvanced, netplay.OutcomeCatchUp: // render from the game
case netplay.OutcomeStalled, netplay.OutcomeWaited:   // nothing new this tick
case netplay.OutcomeEnded:                            // see s.Err()
}
```

`Advance` does five things in one tick: receive packets, roll back if a
prediction was wrong, decide whether to stall or wait, simulate one frame, and
send one packet. Render the local player from the newest simulated frame
without interpolation.

Session rules (the section numbers refer to the netplay spec):

| Rule | Behavior |
|---|---|
| Input delay | Local input sampled at frame f is applied at f + D. `AutoDelay` picks D from the handshake round trip with `SuggestDelay`. |
| Prediction window | At most 8 frames past the last confirmed remote input; at the limit the session stalls. |
| Stall timeout | After 5 s stalled, `Err()` is `ErrDisconnect`. |
| Time sync | A peer more than one frame ahead waits one frame at most every 4 ticks. |
| Round trip | Measured from an echoed send timestamp minus the hold time. |
| Checksums | Computed on confirmed frames every 30 frames and exchanged in input packets. |
| No pause | The wire has no pause packet. A silent peer stalls the other, and the timeout ends the match. |

Without a relay, a checksum mismatch ends the match with a `*DesyncError`
(`errors.Is(err, ErrDesync)`) that carries the local state.

## Presentation after a rollback

After any `Advance` where `s.ChangedFrom() >= 0` (a rollback can happen on a
stalled tick too), feed the ledger:

```go
if from := s.ChangedFrom(); from >= 0 {
    events = s.EventsFrom(from, events[:0])
    ledger.Reconcile(from, s.Frame(), events)
}
```

`EventLedger` calls `Start(e, late)` for a new event, `late` frames behind the
newest frame, and `Cancel(e)` for an event that no longer happens. An event
that survives untouched gets no callback. Keys are `(Frame, Kind, Source)`; a
changed `Data` payload cancels and restarts the event.

## Relay

```go
relay, _ := netplay.NewRelay(netplay.RelayConfig[Pad]{
    NewGame: func() netplay.Game[Pad] { return newGame() },
    Codec:   padCodec{}, Identity: sim.SimulationIdentity,
    Peers:   [2]netplay.Transport{toPlayer0, toPlayer1},
})
relay.AddSpectator(toSpectator)
// every few milliseconds:
relay.Tick(now)
```

The relay forwards every input packet whole, so redundancy still works. It
logs only the first copy of each frame's input. It simulates a native copy of
the game, at most 30 frames per tick, and compares each peer's checksum with
its own. On a mismatch it sends its `Save` blob for that frame in fragments
until the peer acknowledges; the peer loads it and re-simulates to the present
at most 30 frames per tick, and gives up after 1 s. A second desync in one
match ends it: both peers see `ErrRemoteEnded`, and `Report()` carries the
replay. `Report()` also lists, per player, the inputs that arrived after their
due time plus the input delay, and sets `Flagged` after repeated late inputs.
An input is not counted if the other player had not yet sent that frame, since
waiting on the other player makes an honest player late too.

Spectators receive confirmed inputs after `SpectatorDelay` (default 3 s).
`netplay.Spectator` applies the stream to a game.

## Replays and simulation identity

`Relay.Replay()` returns a `Replay`; `Marshal` and `UnmarshalReplay` give the
versioned binary format. `VerifyReplay` re-simulates it and compares every
recorded checksum. It returns `ErrIdentityMismatch` before running any frame if
the replay was recorded under a different identity.

The identity covers the sources, the data tables, the Go version and the det
version. Generate it with `go generate`:

```go
//go:generate go run m31labs.dev/gosx/netplay/cmd/netplay-identity -src step.go,state.go -data moves.json -det v1.2.0
```

This writes `identity_gen.go` with `SimulationIdentity`. List every file that
changes what the simulation computes. Source files are hashed with CRLF folded
to LF; data files are hashed byte for byte. Peers with different identities do
not start a match.

## Testing your game

`LossyNet` is a seeded in-process network with one-way delay, jitter, loss and
reordering; `NewLoopbackPair` is a zero-latency pair. Drive two sessions by
setting the network clock with `SetNow` and calling `Advance` for each peer.
The package's own fuzz test does exactly this for 10,000 seeds:

```sh
make test-netplay                                  # 10,000 sessions, a few seconds
NETPLAY_FUZZ_SESSIONS=300 make test-netplay-wasm   # same suite under js/wasm
make bench-netplay
```

Measured on one machine (Intel Core Ultra 9 285), with the package's tiny test
game: an 8-frame rollback takes 183 ns native and 741 ns under js/wasm with
Node; one tick of two sessions takes 284 ns native and 1.0 us under wasm; none
of them allocates. Your game's `Step` cost will dominate. Measure the worst-case
8-frame re-simulation of your own game on your target device.
