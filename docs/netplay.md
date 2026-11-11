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

The relay logs only the first copy of each frame's input and forwards a packet
built from that log (see below), so redundancy still works. It simulates a native copy of
the game, at most 30 frames per tick, and compares each peer's checksum with
its own. On a mismatch it sends its `Save` blob for that frame in fragments
until the peer acknowledges; the peer loads it and re-simulates to the present
at most 30 frames per tick, and gives up after 1 s. A second desync in one
match ends it: both peers see `ErrRemoteEnded`, and `Report()` carries the
replay. `Report()` also lists, per player, the inputs that arrived after their
due time plus the input delay, and sets `Flagged` after repeated late inputs.
An input is not counted if the other player had not yet sent that frame, since
waiting on the other player makes an honest player late too.

### What the relay validates and forwards

The relay never forwards a sender's bytes. After it checks an Input packet, it
logs the entries and sends the other player a new packet built from its own log,
starting at the frame that player last acknowledged. So a player cannot change
what the other receives after the relay logged it, and a sender that never
resends cannot leave the other player behind a lost packet. The wire format
is unchanged.

The relay rejects or distrusts an Input packet, and counts a violation for the
sender (`PlayerReport.Violations`, by `ViolationKind`), when it:

- has the wrong tag or length (dropped; nothing in it is trusted);
- starts above the highest ack the relay has forwarded from the other player
  (`ViolationNoResend`; an honest sender resends from that ack. Its ack is
  ignored, and entries that extend the log are still logged);
- carries frames beyond the other player's log + `PredictionWindow` + the
  sender's input delay + `InputSlack` (`ViolationFuture`; dropped whole);
- acknowledges frames the relay has not logged (`ViolationImpossibleAck`; the
  ack is ignored); or
- repeats a logged frame with different bytes (`ViolationConflict`).

`RelayConfig.PredictionWindow` and `MaxInputDelay` must be at least the
sessions' values. A relay with smaller values rejects honest packets.

### Facts the app uses to judge a match

`Report().Players[i]` is the relay's view of one player. The relay records
facts; the app decides what they mean. Each fact has a caveat.

| Field | Meaning and caveat |
|---|---|
| `Violations`, `ViolationCounts`, `FirstViolation`, `FirstViolationFrame`, `ConflictingEntries`, `ImpossibleAcks` | Protocol violations by this player, attributed to the sender. A player with any of these is the suspect, whatever the `Desyncs` counts say. Packet loss alone does not cause them; a build with a different `PredictionWindow` or `MaxInputDelay` than the relay's does. |
| `Desyncs`, `Resyncs`, `FirstDesyncFrame` | Checksum frames where this player's checksum disagreed with the relay's native copy, resyncs it acknowledged, and the first such frame (0 if none). They show that a build diverged, not why. Read them together with the violation fields. |
| `AckedFrame`, `ForwardedAck` | The highest ack in this player's packets that passed validation, and the ack the relay sent to the other player (which never exceeds the frames the relay logged for that player). |
| `AckLag`, `MaxAckLag`, `AckLagFlagged` | Distance between the other player's frames that the relay forwarded at least `AckLagGrace` ago (default 1 s) and the frames this player acknowledged, measured when its valid packets arrive. `AckLagFlagged` is set above `AckLagLimit()` = `PredictionWindow + MaxInputDelay + AckLagSlack` (defaults 8, 3 and 30; `AckLagSlackNone` for 0). A client that keeps acknowledging frame 0 makes the honest peer stall at 240 unacknowledged frames; this flag lets the app refuse that forfeit. A link that drops everything for longer than the grace and slack also trips it. |
| `LateInputs`, `MaxLateness`, `Flagged` | Inputs that arrived later than their due time plus the input delay, counted only if the other player had already sent that frame. Caveat: honest players on links above about 100 ms one way can set `Flagged` (the due time assumes a short path), so do not forfeit on it alone. |
| `LogGap`, `MaxLogGap`, `SlowGaps`, `CrawlFlagged` | The time this player has held the match back: the time since the later of its last log growth and the moment the other player's log became longer than its own (the clock starts when the player is behind, not before). `MaxLogGap` is the longest such gap; `SlowGaps` counts gaps longer than `CrawlAfter` (default 1.5 s); `CrawlFlagged` is set after `CrawlGaps` (default 3) of them. A client that crawls (one tick every 2 s) collects slow gaps without ever stalling long enough for a stall timeout. A pause of the relay itself (no `Tick` for over 500 ms) restarts the clocks. A link outage that silences one player is indistinguishable from that player crawling. Check `Violations` first. |
| `LastLogAt`, `LastLogTick` | The relay clock (the `now` given to `Tick`) and the `Tick` count when this player's log last grew. The player whose log stopped first stalled first. Check `Violations` first: a player with violations is the suspect whatever its log times say. |
| `LastPacketAt`, `LastPacketTick` | The same for any valid packet from the player. |

The violation, ack and desync facts assume an authenticated transport (DTLS
for WebRTC, TLS for the WebSocket) and a seat that only its owner can use: on
an open link, another host can inject packets that the relay attributes to the
seat.

A packet that declares an input delay above `MaxInputDelay` is clamped and
counted (`ViolationBadDelay`), and a delay that changes after the first input
is ignored and counted (`ViolationDelayChange`).

The times come from the `now` you pass to `Tick`. They are facts for the app's
verdict, not simulation state: never feed them to `Game.Step`.

### Ending a match

When your game rules decide the match is over, call `relay.Finish()` on the
goroutine that calls `Tick`. It sends the End packet (reason `EndMatchOver`) to
both players at once, four copies, and every `Session` ends with
`ErrMatchOver` (`s.MatchOver()` is true). That is a normal end, not a failure:
`ErrDisconnect` and `ErrRemoteEnded` still mean a stall and a desync. A
datagram can be lost, so the relay repeats End by relay time: 20 more copies,
at least 100 ms apart (`EndRepeats`, `EndRepeatEvery`), one per `Tick` at most.
**Keep calling `Tick` until `relay.Draining()` is false.** The relay never
ticks itself; if you stop at once, a client that lost all four copies ends
with `ErrDisconnect` after its stall timeout. `Finish` does nothing if the
match already ended (for example by a desync). Upgrade clients before a relay
calls `Finish`: an older client treats every End packet as `ErrRemoteEnded`.

### Sessions

A session drops a remote input that runs further ahead than the prediction
window, its input delay and 4 frames of slack (`Stats().FutureInputs`), and
ignores an ack for frames it has not produced (`Stats().ImpossibleAcks`).
The handshake has its own limit, `Config.HandshakeTimeout` (default 30 s, at
least `StallTimeout`), so a player whose scene loads late does not force a
restart. With `AutoDelay`, the session takes the minimum of `RTTSamples`
(default 4) round-trip samples and ignores any sample above `RTTCap` (default
500 ms, counted in `Stats().RTTIgnored`). If no believable sample arrives within
`RTTWait` (default 1 s) of the peer's first packet, it uses `DelayFallback`
(default 1; `Stats().DelayFallbacks`). So a peer that never echoes cannot hold
the handshake open, and one that fakes its echoes can raise the delay only by
the cap: a peer that adds less than the cap to every echo still gets a higher
delay for you, up to the maximum. A round-trip sample whose echoed timestamp predates the session's
first packet is ignored (`Stats().StaleEchoes`), so a restarted session does
not start with a half-second round trip and a delay of 3.

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

## Network transport: WebRTC with a WebSocket fallback

The engine above needs a `Transport`. For real players, GoSX provides one that
uses an unreliable WebRTC data channel between each browser and the relay, and
falls back to the hub WebSocket where WebRTC cannot connect. You write no
JavaScript.

| Piece | Where | Dependencies |
|---|---|---|
| `netplay/signal`, `netplay/hubnet`, `netplay/dgram` | core module | standard library and the hub |
| `netplay/client` | core module, browser (Go WASM) | no pion |
| `bootstrap-feature-netplay.js` | runtime chunk, loaded on demand | none |
| `netplay/webrtc` | nested module `m31labs.dev/gosx/netplay/webrtc` | pion/webrtc v4 |

The relay program imports the nested module; the core module and your WASM
build never import pion. Add it to the relay's `go.mod` with
`go get m31labs.dev/gosx/netplay/webrtc`.

### Relay (Go, native)

```go
srv, err := webrtc.NewServer(webrtc.Config{
    ICEServers: []signal.ICEServer{
        {URLs: []string{"stun:stun.example.org:3478"}},
        {URLs: []string{"turn:turn.example.org:3478"}, Username: user, Credential: secret},
    },
    FallbackAfter: 4 * time.Second, // sent to browsers; 0 lets them choose
})
h := hub.New("match")
srv.AttachHub(h) // signaling events and the WebSocket fallback
h.On("join", func(ctx *hub.Context) { /* pair players by ctx.Client.ID */ })

// Seats are named by an unguessable token your matchmaker gave each player,
// which the client sends as Options.Session. Tell the server about a leave
// with Leave (not Remove), so the seat survives a hub reconnect.
rebind := func(old, now string) {
    if err := srv.Rebind(old, now); errors.Is(err, webrtc.ErrClientLive) {
        return // the leave handler finishes it
    }
}
srv, _ = webrtc.NewServer(webrtc.Config{
    OnHello: func(id, session, previous string) {
        if previous != "" { rebind(previous, id) } // the player reconnected
    },
})
h.On("leave", func(ctx *hub.Context) {
    if now := srv.Leave(ctx.Client.ID); now != "" { rebind(ctx.Client.ID, now) }
    // when the reconnect grace ends and nobody came back: srv.Remove(id)
})

// once two clients are paired:
relay, _ := netplay.NewRelay(netplay.RelayConfig[Pad]{
    NewGame: newGame, Codec: padCodec{}, Identity: sim.SimulationIdentity,
    Peers: [2]netplay.Transport{srv.Link(idA), srv.Link(idB)},
})
```

`srv.Link(clientID)` is the relay's `Transport` for one player. It sends on the
data channel while the client uses it and on the WebSocket when the client has
fallen back, and it reads both. `link.Mode()` and `link.Degraded()` report
which. The STUN and TURN servers go to pion on the relay and to each browser in
a `netplay.config` message, so TURN credentials stay in your server config.
Hand out short-lived TURN credentials: they reach the client.

`webrtc.Config` also has `ConfigureICE`, a hook on pion's `SettingEngine`, for a
UDP port range, `SetNAT1To1IPs` behind a fixed public address, or interface
filters, and `MaxPeers`, `ConnectTimeout` and `QueueLen` limits. Allow the
relay's UDP range in your firewall. The hub's `MaxMessagesPerSecond`, if set,
must allow the fallback path: two players send 60 frames per second each.

### Releasing the nested module

`netplay/webrtc` is its own Go module, so it needs its own tag. After
tagging the core release `vX.Y.Z`, set `m31labs.dev/gosx vX.Y.Z` in
`netplay/webrtc/go.mod` (the `replace m31labs.dev/gosx => ../..` stays for
tests), merge, and tag the same commit `netplay/webrtc/vX.Y.Z`. Without that
tag a consumer must pin a pseudo-version. The tag is pushed by the releaser,
not by a feature PR.

### Page (Go)

```go
ctx.Runtime().RequireFeature("netplay") // loads bootstrap-feature-netplay.js on this page only
```

### Browser (Go WASM)

```go
conn, err := client.Dial(client.Options{
    HubURL:  "wss://example.com/gosx/hub/match",
    Session: seatToken, // an unguessable token from your matchmaker, not the default "netplay"
    OnMode: func(m client.Mode) { /* ModeWebRTC, or ModeWebSocket: show "degraded" */ },
})
s, _ := netplay.NewSession(netplay.Config[Pad]{Transport: conn /* ... */})
```

`Options.Header` is `hubclient.Header`: `http.Header` in native builds (so
existing code compiles) and a plain `map[string][]string` in js/wasm builds, so
a browser client does not link `net/http` or `crypto/tls`. The native build
sends it on the WebSocket handshake; the browser ignores it.
`netplay/client` tests fail if a js/wasm build links `net/http`, `crypto/tls`,
`gorilla/websocket` or the hub server, or if the minimal client is over a size
budget (4.6 MB raw).

`Dial` returns at once. Until a path is ready, `Send` drops datagrams and
`Recv` returns nothing, as a lossy network would; the session handshake
retries. `conn.Mode()` is `ModeConnecting`, `ModeWebRTC` or `ModeWebSocket`.
`conn.Degraded()` is true on the WebSocket.

### Fallback rules

- The browser makes the offer; the relay answers. Both ends create the data
  channel as negotiated (id 0, `ordered: false`, `maxRetransmits: 0`).
- If the channel is not open within `FallbackAfter` (default 4 s), if the
  browser has no `RTCPeerConnection`, if the page did not load the chunk, or if
  the relay replies `netplay.failure`, `Conn` switches to hub binary frames
  (first byte `0x00`, then the packet) and calls `OnMode(ModeWebSocket)`.
- Both ends read both paths all the time, so no packet is lost at a switch. The
  relay answers on the WebSocket as soon as the client sends there, even if
  pion still reports the channel open.
- If the channel opens later, `Conn` moves back to WebRTC.
- The WebSocket is ordered and reliable, so loss becomes delay (head-of-line
  blocking). Rollback still works, but the match feels worse; tell the player.
- If the hub connection itself drops, hub reconnect gives the client a new ID.
  `Conn` sends its hello again on every hub connection. On the relay,
  `webrtc.Config.OnHello` is called with `(clientID, session, previousClientID)`.
  `Server.Rebind(oldID, newID)` moves the seat: the same `Link`, with its data
  channel and its WebSocket path, now serves the new ID, so the relay keeps its
  transport. `Rebind` refuses (and changes nothing) unless the old client was
  reported gone with `Server.Leave` (`ErrClientLive`), both clients announced
  the same session, the session is not the default label `"netplay"`
  (`ErrDefaultSession`; set `Options.Session`), and the new client has no
  `Link` yet (`ErrSeatTaken`; call `Link` after `Rebind`). Call `Leave` from
  your hub leave handler instead of `Remove`, and `Remove` only when the
  reconnect grace has ended (at most the session stall timeout, 5 s by
  default): `Remove` closes the data channel and drops the `Link`. After a
  fallback to the WebSocket, `Conn` uses the WebRTC channel again only if the
  channel opens later; it does not renegotiate.
- A session name is a label, not a credential: any client can announce any
  name, and a client that knows a seat's token can take the seat once its owner
  has left. Use an unguessable seat token from your matchmaker as the session
  name, or check the new client's authenticated identity before `Rebind`.

### Signaling and limits

Signaling messages are hub text events: `netplay.hello`, `netplay.config`,
`netplay.offer`, `netplay.answer`, `netplay.candidate` and `netplay.failure`.
`netplay/signal` validates every message before use: payload at most 24 KiB,
SDP at most 16 KiB and 256 lines, candidates at most 1 KiB, session ids of
`[A-Za-z0-9._-]` up to 64 bytes, at most 8 ICE servers with `stun`, `stuns`,
`turn` or `turns` URLs. The relay accepts at most 64 trickled candidates per
connection. A datagram never exceeds 1,200 bytes. When more than 16 KiB sits
unsent in the data channel, `Send` drops the datagram, because an old input is
worth less than a fresh one.

### Testing

```sh
make test-netplay-webrtc    # nested module: pion loopback matches, WebSocket-fallback match
make test-netplay-browser   # headless Chromium plays a Go WASM peer through the relay
```

The module tests run full matches through a `netplay.Relay` over real pion data
channels on loopback and over the WebSocket fallback, and require zero desyncs.
`test-netplay-browser` needs Playwright in `node_modules`; set `NETPLAY_CHROME`
to a Chromium or `chrome-headless-shell` binary if Playwright's own download
does not run. Headless Chromium on loopback does not exercise NAT, TURN or
mobile networks: test those on real devices.
