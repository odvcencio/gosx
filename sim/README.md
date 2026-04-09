# gosx/sim

Server-authoritative game simulation over gosx hubs.

## Usage

```go
runner := sim.New(matchHub, &myGame{}, sim.Options{TickRate: 60})
runner.RegisterHandlers()
runner.Start()
defer runner.Stop()

// After match:
replay := runner.Replay()
```

Games implement `sim.Simulation`:

```go
type Simulation interface {
    Tick(inputs map[string]Input)
    Snapshot() []byte
    Restore(snapshot []byte)
    State() []byte
}
```

The runner handles tick scheduling, input collection, state broadcast,
snapshot storage, replay recording, and spectator sync.

## Features

- **Fixed-rate tick loop** — deterministic simulation at configurable tick rate
- **Input collection** — hub "input" events routed to simulation
- **State broadcast** — "sim:tick" events with frame number + serialized state
- **Snapshot ring** — 128-frame history for rollback support
- **Replay recording** — full input log for match replay
- **Spectator sync** — joining clients receive current snapshot
