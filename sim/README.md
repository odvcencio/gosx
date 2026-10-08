# gosx/sim

Server-authoritative game simulation over gosx hubs.

## Usage

```go
runner := sim.New(matchHub, &myGame{}, sim.Options{
    TickRate:      60,
    StateEncoding: sim.StateEncodingJSON, // when State returns valid JSON
})
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

## Tick observation

`sim.Options.Observer` receives one `TickEvent` after each whole tick: input
drain, simulation, one snapshot, optional replay, state serialization and the
existing broadcast. `StateBytes` is the state size before hub encoding; `Inputs`
counts drained players. The observer receives aggregate values, not player IDs
or state contents. It runs synchronously outside runner state locks and must
return promptly. Stop the runner from its owner, not from its callback.

Telemetry can bind a declared loop meter without changing simulation ownership:

```go
meter, err := loopKind.Instance() // after declaring the kind and building the app
if err != nil { /* handle capacity or shutdown */ }
runner := sim.New(matchHub, &myGame{}, sim.Options{
    TickRate: 60,
    Observer: tel.SimObserver(meter),
})
runner.Start()
// When the game's owner finishes:
runner.Stop()
health := meter.Health() // lifetime health, before closing the meter
_ = health
meter.Close()
```

`Options.Clock` accepts the same clock interface as telemetry, including its
deterministic test clock. Lag is elapsed start time minus the ticker's scheduled
deadline, clamped at zero. Slow work may drop periods; the runner executes no
catch-up ticks. A nil observer skips timing reads. `game.NewRunner` forwards
these options unchanged. Metering that runner covers its authoritative runtime;
do not also meter the runtime's internal `Step`.

## Features

- **Fixed-rate tick loop** — deterministic simulation at configurable tick rate
- **Input collection** — hub "input" events routed to simulation
- **State broadcast** — "sim:tick" events with frame number + serialized state
- **JSON state encoding** — valid JSON state can be embedded directly instead of base64-encoded
- **Snapshot ring** — 128-frame history for rollback support
- **Replay recording** — full input log for match replay
- **Spectator sync** — joining clients receive current snapshot
