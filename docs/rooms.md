# Rooms: online multiplayer with bots and prediction

Package `m31labs.dev/gosx/room` runs server-authoritative game rooms over hub
WebSockets. Package `m31labs.dev/gosx/game/netsync` is the client: it predicts
the local player on a copy of the simulation and corrects itself from the
server's snapshots. Both read the same `mode.Mode`, so a static game client can
connect to your server from another origin, and it plays on alone, against
bots, when the server is out of reach.

Every room is playable by one person. A room starts its countdown as soon as
one person is connected and puts a bot in every empty seat. People who arrive
later take over a bot's seat.

```text
browser (any origin)                         your server
┌──────────────────────────┐   wss    ┌──────────────────────────────┐
│ netsync.Client           │ ───────► │ room.Server  (Origin check)  │
│  predicted sim + inputs  │  inputs  │  Room: seats, bots, lifecycle│
│  Interpolator (others)   │ ◄─────── │  fixed tick: sim.Simulation  │
└──────────────────────────┘ keyframes└──────────────────────────────┘
                              + deltas
```

## 1. Describe the mode once

Put your modes in a package that both the server and the js/wasm client
import. A mode says how many seats it has, how to build the simulation from a
seed, and how a bot plays a seat.

```go
package modes

var Duel = mode.Mode{
    Name:      "duel",
    Seats:     2,
    Roles:     []string{"red", "blue"},
    TickRate:  30,                       // fixed steps per second
    InputSize: 2,                        // bytes of held input per tick
    Countdown: 3 * time.Second,
    Duration:  2 * time.Minute,          // or set Over
    NewSim: func(seed uint64) sim.Simulation {
        return newArena(seed)            // a *game.Runtime works
    },
    NewBot: func(seat int, role string) mode.Bot {
        return mode.BotFunc(func(ctx mode.BotContext, dst []byte) []byte {
            a := arenaOf(ctx.Sim)        // read-only view of the world
            return append(dst, a.steerToward(ctx.Seat)...)
        })
    },
    Results: func(s sim.Simulation) []byte { return arenaOf(s).scoreJSON() },
}
```

The simulation must be deterministic: the server and every client run it from
the same seed with the same inputs and must get the same bytes. Use integer
fixed-point or the `det` toolkit for floats, and iterate inputs in seat order
(`mode.PlayerID(seat)` gives the key, `"p0"`, `"p1"`, ...).

An input is the held state for one tick (which buttons are down), not an
edge. Derive presses inside the simulation from the previous tick's held
state. Then repeating a player's last input is always a safe prediction, which
is what the server does when an input is late and what the client does for
other players.

`Snapshot` must return everything a restore needs, and the client restores a
snapshot several times a second. A compact fixed-layout binary encoding keeps
deltas small; `room/roomtest` shows one.

## 2. Run the server

```go
srv, err := room.NewServer(room.Config{
    Modes:          []mode.Mode{modes.Duel},
    AllowedOrigins: []string{"https://mygame.example", "https://*.portal.example"},
})
if err != nil {
    log.Fatal(err)
}
mux := http.NewServeMux()
mux.Handle("/play/", http.StripPrefix("/play", srv))
```

The server answers:

| Request | Purpose |
|---|---|
| WebSocket `GET /play/ws?...` | join a room (query below) |
| `GET /play/rooms[?mode=duel]` | JSON list of public rooms, readable cross-origin by allowed origins |

Join query parameters, all optional:

| Parameter | Meaning |
|---|---|
| `mode=duel` | mode name; default is the first mode |
| `code=7K3QX` | join that room |
| `private=1` | create a private room instead of quick-matching |
| `resume=<token>` | reclaim a seat after a drop |
| `name=Ada` | display name, at most 24 characters |
| `spectate=1` | watch without a seat |

With no `code` and no `private`, the join quick-matches: it picks the public
room of that mode that is still gathering people (most people first), then a
running room where a bot holds a seat, and creates a room when there is none.

Join codes are five characters of Crockford base32 (`0-9`, `A-Z` without `I`,
`L`, `O` and `U`). `room.NormalizeCode` reads lower case, dashes and spaces,
and the look-alikes `I`, `L` and `O` as `1`, `1` and `0`, so a code read aloud
still works.

### Configuration

Zero values select these defaults.

| Field | Default | Meaning |
|---|---|---|
| `MaxRooms` | 256 | live rooms; more joins are refused with code 4006 |
| `MaxSpectators` | 8 | per room; -1 for none |
| `ReconnectGrace` | 30 s | how long a dropped person's seat waits |
| `ConnectGrace` | 10 s | how long a reserved seat waits for its socket |
| `IdleTimeout` | 60 s | close a room with nobody connected |
| `ResultsHold` | 10 s | results phase before the room closes |
| `InputDelay` | 2 ticks | input buffer clients keep at the server |
| `SnapshotEvery` | 1 tick | snapshot spacing |
| `KeyframeEvery` | 1 s of snapshots | keyframe spacing; deltas in between |
| `MaxCatchUp` | 8 | ticks one room runs per wake after a stall |
| `MaxMessagesPerSecond` | 2 × tick rate + 20 | inbound frames per client; more closes the socket |
| `ConnectsPerMinute` | 30 | WebSocket connections per client address |
| `CreatesPerMinute` | 10 | new rooms per client address |
| `TrustForwardedFor` | off | key limits by `X-Forwarded-For` behind your proxy |

## 3. Host it for clients on other origins

A browser game served from a CDN, a game portal or a jam page's iframe runs on
an origin that is not your server's. Browsers send that origin in the `Origin`
header of the WebSocket upgrade, and the server only accepts the upgrade when
the origin is its own or appears in `AllowedOrigins`:

- `https://mygame.example` matches exactly (a default port may be written or
  left out).
- `https://*.portal.example` matches any subdomain of `portal.example`, with
  the same scheme and port, but not `portal.example` itself.
- `null` admits sandboxed iframes without `allow-same-origin`. Every such
  iframe on every site sends `null`, so add it only if your host needs it.

A request with no `Origin` comes from a non-browser client and is allowed
unless `RequireOrigin` is set. An unlisted origin gets HTTP 403 before any
seat is reserved. The same allowlist sets the CORS headers on `/play/rooms`.

Serve the WebSocket over TLS (`wss://`); a page served over HTTPS cannot open
`ws://`. Terminate TLS at your ingress or reverse proxy and pass the upgrade
through. If the proxy sets `X-Forwarded-For`, set `TrustForwardedFor` so rate
limits see client addresses instead of the proxy's.

The hub-level pieces are usable without rooms: `hub.NewOriginAllowlist(...)`
and `Hub.CheckOrigin = allow.Check` give any hub the same policy, and
`Hub.DisconnectWithCode` closes a client with a code its browser can read.

## 4. Connect from the client

```go
client, err := netsync.New(netsync.Config{
    URL:   "wss://rooms.mygame.example/play/ws",
    Mode:  modes.Duel,
    Join:  netsync.Join{Name: playerName},          // or Code, Private, Spectate
    Input: func(tick uint64, dst []byte) []byte {  // held input for this tick
        return append(dst, pad.Bits())
    },
    Store: storage.New("mygame", storage.Session()), // keeps the resume token per tab
})

// Every render frame:
frame := client.Frame(delta)
world := client.Sim()        // predicted simulation; render the local player from it
alpha := client.Alpha()      // fraction to the next tick, for smooth rendering
```

`Frame` does all the network work on the game loop, without goroutines on
js/wasm:

1. It reads the frames that arrived and keeps the newest authoritative
   snapshot.
2. When a new snapshot arrived, it restores it and replays the local inputs
   the server has not applied yet (reconciliation). Other players are
   predicted by repeating the input the server last applied for them.
3. It steers its clock so each local input reaches the server about
   `InputDelay` ticks before it is due. The target comes from ping round trips
   and the server's tick timeline, and the server reports how much of this
   seat's input it holds in every snapshot, which corrects for asymmetric
   routes. Small errors speed the clock up or slow it down by up to 15%;
   errors over a fifth of a second (at least 4 ticks) resync at once.
4. It runs the due ticks with a `game.Clock` under `game.CatchUpLag`, which
   never drops a tick, so the client's tick count stays aligned with the
   server's.
5. It sends the newest inputs, with up to `Redundancy` (default 8) earlier
   ticks the server has not confirmed, so a lost or late frame costs nothing.

### Other players: interpolation

Prediction can make other players jump when their input changes. Render them
from an `Interpolator` instead, a few ticks behind the server:

```go
ip := netsync.NewInterpolator(3, 32) // 3 values per entity, 32 snapshots

cfg.OnSnapshot = func(tick uint64, s sim.Simulation) {
    ip.Begin(tick)
    for _, p := range arenaOf(s).players {
        ip.Add(p.ID, p.Pos[:])
    }
}

// Every render frame:
ip.Sample(client.ServerTick()-2, func(id uint32, v []float32) { drawRemote(id, v) })
```

`OnSnapshot` sees the simulation restored to each authoritative snapshot,
before the local replay. `Begin`, `Add` and `Sample` do not allocate once the
buffer has warmed.

### Play starts at once

A player never waits on the network. From the first `Frame`, the client runs
a local match: the player in seat 0, a bot in every other seat. Meanwhile the
first connection runs in the background. When it connects, the local match
keeps running through the room's lobby and countdown, and the client hands
over to the room's match as soon as that match starts. If the server never
answers, the same local match simply continues.

`client.Local()` reports that `Sim()` is a local match. While the client is
connected but still playing locally, `client.Room()` already describes the
server's room (its code, phase and seats, with `Local` set), so a game can
show "joining room 7K3QX, starting in 2 s" over local play. Set
`WaitForServer` to keep `Sim()` nil until the room's match starts instead,
for games that show a lobby screen.

### Status, reconnect and offline play

`client.Status()` is one of:

| Status | Meaning |
|---|---|
| `StatusConnecting` | first dial, up to `ConnectTimeout` (5 s); play runs locally meanwhile |
| `StatusOnline` | in a room; `client.Room()` has the code, phase and seats |
| `StatusReconnecting` | the link dropped; prediction continues while the client redials with its token |
| `StatusOffline` | playing locally: the current world continues with a bot in every other seat |
| `StatusClosed` | the match ended (`Room().Results` holds the result), the seat moved to a newer tab, or `Close` was called |

A drop is noticed when the socket closes or nothing arrives for
`StallTimeout` (5 s). The client redials with `resume=<token>` and backoff for
up to `ReconnectTimeout` (10 s); the server keeps the seat for
`ReconnectGrace` (30 s) with a bot covering it. If the room refuses the client
for good (closed, full, seat expired), or the time runs out, the client falls
back to offline play and the room's world continues locally from where it
was. Set `NoFallback` to end in `StatusClosed` instead.

`client.Stats()` reports round trip, jitter, predicted and authoritative
ticks, the input buffer the server holds (`Lead`), reconciles, replayed ticks,
resyncs, snapshots, and bytes in and out. Show a few of them in a debug
overlay.

## 5. Lifecycle

```text
lobby ──first person connected (+ LobbyWait)──► countdown ──► running ──Duration or Over──► results ──► closed
```

- **lobby**: seats fill as people join. The countdown starts when one person
  is connected, after `LobbyWait` (default 0, so a lone person never waits).
  `Room.Start` starts it at once.
- **countdown**: `Countdown` long (default 3 s; negative skips it). People can
  still join.
- **running**: empty seats get bots, the simulation starts from the room's
  seed at tick 0, and the room ticks at the mode's rate. A late arrival takes
  a bot's seat unless the mode sets `NoHotJoin`.
- **results**: after `Duration`, or the first tick `Over` reports true. The
  mode's `Results` payload goes to every client.
- **closed**: after `ResultsHold`, or when nobody has been connected for
  `IdleTimeout`. Clients get close code 4000.

A person who drops keeps their seat for `ReconnectGrace`; a bot plays it
meanwhile. After the grace, the seat stays a bot for the rest of the match
(in the lobby it empties) and the token stops working.

## 6. Wire protocol

Frames are binary WebSocket messages starting with `0x00` (the hub routes
other first bytes to CRDT sync) and a kind byte. `room/wire` encodes and
decodes them; its decoders are fuzzed and refuse hostile input with
`ErrMalformed`.

| Kind | Direction | Content |
|---|---|---|
| `w` Welcome | server → client | protocol version, seat (-1 spectator), seat count, tick rate, input delay, seed, mode, room code, resume token |
| `r` Room | server → client | phase, time left, tick, each seat's kind, role, name and connection, results |
| `k` Keyframe | server → client | epoch, tick, seat table, full simulation snapshot |
| `d` Delta | server → client | epoch, tick, seat table, delta against the epoch's keyframe |
| `i` Input | client → server | first tick, up to 32 ticks of held input |
| `p` / `o` Ping / Pong | both | client clock echo, server tick timeline |

A delta always encodes against its epoch's keyframe, never against another
delta, so a client that misses one recovers with the next. The hub's snapshot
lane gives a newly connected or slow client the current keyframe and then only
the newest delta, instead of a backlog. The delta format copies runs from the
keyframe and sends the rest literally; a fixed-layout snapshot encodes each
unchanged run in two or three bytes. When a delta would be more than half the
state, the server sends a keyframe instead.

Close codes a client can read from `CloseEvent.code`:

| Code | Meaning | Client action |
|---|---|---|
| 4000 | match over | closed |
| 4001 | seat resumed by a newer connection | closed |
| 4002 | reconnect grace ran out | offline |
| 4004 | no room with that code | offline |
| 4005 | room full | offline |
| 4006 | server at room capacity | offline |
| 4007 | unknown mode or bad request | offline |
| 4008 | room closed: nobody connected | offline |
| 4009 | server shutting down | reconnect, then offline |
| 4029 | too many connections | offline |

## 7. Bandwidth

Measured with `go test ./game/netsync -run TestBandwidth -v`: the
`room/roomtest` bodies game, where every body moves every tick (a busy scene),
a snapshot every tick, and a keyframe every second. Byte counts include the
2-byte WebSocket header per server frame and the 6-byte masked header per
client frame, but not TCP, TLS or IP overhead.

| Tick rate | People | Props | State bytes | Keyframe B | Delta B | Down KiB/s per client | Up KiB/s per client | Server out KiB/s per room |
|---|---|---|---|---|---|---|---|---|
| 30 Hz | 2 | 0 | 52 | 66 | 52 | 1.6 | 0.5 | 3.1 |
| 30 Hz | 2 | 32 | 692 | 706 | 356 | 11.5 | 0.5 | 22.9 |
| 30 Hz | 4 | 0 | 92 | 114 | 84 | 3.4 | 0.5 | 13.4 |
| 30 Hz | 4 | 32 | 732 | 754 | 389 | 13.1 | 0.5 | 52.1 |
| 30 Hz | 8 | 0 | 172 | 210 | keyframes only | 6.2 | 0.5 | 49.3 |
| 30 Hz | 8 | 32 | 812 | 850 | 455 | 19.5 | 0.5 | 155.1 |
| 60 Hz | 2 | 0 | 52 | 66 | 53 | 3.2 | 1.2 | 6.3 |
| 60 Hz | 2 | 32 | 692 | 706 | 356 | 22.4 | 1.2 | 46.1 |
| 60 Hz | 4 | 0 | 92 | 115 | 84 | 6.6 | 1.2 | 26.8 |
| 60 Hz | 4 | 32 | 732 | 754 | 389 | 25.4 | 1.2 | 104.4 |
| 60 Hz | 8 | 0 | 172 | 211 | keyframes only | 12.0 | 1.2 | 98.8 |
| 60 Hz | 8 | 32 | 812 | 851 | 456 | 38.3 | 1.2 | 313.6 |

"Keyframes only" rows: with eight people and nothing else, every body changes
most of its bytes each tick, so no delta came under half the state.

Downstream cost per client is roughly snapshot size × snapshot rate, so the
biggest levers are a compact snapshot (quantize positions, leave out what the
client can derive) and `SnapshotEvery: 2` at 60 Hz, which halves it.
Upstream stays near 0.5 KiB/s at 30 Hz whatever the room size.

## 8. Test without a network

`room.Server.ConnectLocal` joins a room in-process with the same frames and
delivery rules as a socket, and `netsync.NewSimConn` puts a deterministic
simulated network around any connection: separate up and down delay, jitter
that reorders frames, and loss of inputs, pings and snapshots (lifecycle
frames keep TCP's order). With `room.Config{Manual: true, Now: clock}` and
`netsync.Config{Now: clock, Dial: ...}`, a test drives both sides from one
virtual clock and the same seed gives the same run. `room/roomtest` is a tiny
moving-bodies game built on `game.Runtime` for such tests; the package tests
of `game/netsync` show the harness.

`examples/rooms` runs a server and a static client cross-origin on one
machine.

## Limits

- The server runs every room's simulation on its own goroutine. A
  simulation that cannot finish a tick within the tick period makes the room
  fall behind; after one second of backlog the room drops ticks and clients
  resync.
- Each snapshot allocates one frame on the server, because the hub shares
  offered frames across clients without copying. The tick itself, input
  intake, and the client's frame, reconcile and delta decode allocate nothing
  of their own (tests check this with a zero-allocation simulation).
- The client restores and replays on every new snapshot. The cost per frame
  is one restore plus the ticks the client runs ahead (about round trip ÷ tick
  period + `InputDelay`, 7 ticks at 150 ms and 30 Hz).
