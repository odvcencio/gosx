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
deadline, clamped at zero. Slow work may drop periods; by default the runner executes no
catch-up ticks (see `CatchUpBudget` below). A nil observer skips timing reads. `game.NewRunner` forwards
these options unchanged. Metering that runner covers its authoritative runtime;
do not also meter the runtime's internal `Step`.

## Concurrency and ordering

The runner never calls `Tick`, `Snapshot`, `Restore` or `State` at the same
time, so a simulation needs no locks of its own. Spectator joins read their
snapshot between ticks. `Tick` receives a map, and Go randomizes map order. Use
`sim.SortedPlayerIDs(inputs)` or `sim.EachInput(inputs, fn)` whenever order can
change state, or replays diverge.

## Saves, restore and replay

- `runner.Restore(frame, snapshot)` loads a save: it restores the simulation,
  sets the runner frame counter (the next tick is `frame+1`) and restarts the
  replay log from that snapshot.
- `runner.RestoreSnapshot(frame, snapshot)` does the same and returns an error
  when the simulation refuses the snapshot. `Simulation.Restore` has no result,
  so a simulation says no by implementing `sim.CheckedRestorer`
  (`RestoreSnapshot([]byte) error`, which must leave the simulation unchanged
  when it fails); `game.Runtime` does. On a refusal the runner frame and the
  replay log stay as they were. `Restore` drops the error but also leaves them
  alone. Both take the lock a tick holds, so they are safe while the runner runs;
  calling the simulation's own restore beside a running runner is a data race.
- `runner.Replay()` returns a `ReplayLog` with `Initial` (the snapshot before
  `Frames[0]`), `InitialFrame`, `Seed` (from `Options.Seed`) and the per-frame
  inputs. To replay: `Restore(log.Initial)`, seed your RNG from `log.Seed`, then
  `Tick` each frame's `Inputs` in order. The baseline is taken once, at `Start`
  (or the first tick), which costs one extra `Snapshot` call.
- `Options.ReplayMaxFrames` stops recording after N frames and sets
  `ReplayLog.Truncated`. The kept frames still replay exactly. Zero is unbounded.
- `game.Runtime` snapshots store time as integer nanoseconds (`timeNanos`) and
  still read older `timeSeconds` snapshots. Since `"version": 2` they also store
  the clock remainder and the `game.Every` and `game.EveryInterval`
  accumulators, so a replay from `ReplayLog.Initial` runs cadence systems on the
  same ticks. Load a save with `runner.RestoreSnapshot` to learn if the runtime
  refused it.

## Runner options

| Option | Default | Effect |
| --- | --- | --- |
| `Broadcaster` | JSON `sim:tick` via `hub.Broadcast` | Replaces the per-tick broadcast. `sim.NewBinarySnapshotBroadcaster(sim.BinarySnapshotOptions{KeyframeEvery: 30})` uses `hub.BroadcastBinarySnapshot` with a keyframe plus delta. `sim.DecodeBinarySnapshot` decodes those frames in Go. The stock browser runtime does not decode them. |
| `RingSize` | 128 | Rollback snapshots kept. `sim.RingDisabled` (-1) keeps none and skips the per-tick `Snapshot` call. Read with `runner.SnapshotAt(frame)`. |
| `SnapshotEvery` | 1 | Store a ring snapshot every N frames. |
| `BroadcastEvery` | 0 (every tick) | Broadcast at most once per this much wall time. `State` is not built on skipped ticks. |
| `CatchUpBudget` | 0 (off) | Run missed ticks after a late wake, within this wall-time budget per wake. Unpaid ticks appear as `TickEvent.Behind`; debt is capped at one second of ticks. |
| `InputQueue`, `InputQueueDepth` | off, 64 | Queue every input per player and deliver one per tick, oldest first, instead of overwriting. `ReceiveInputAt(player, frame, input)` holds an input until that frame; late stamps deliver at once. |
| `Seed`, `ReplayMaxFrames` | 0 | See replay above. |

`RingSize` uses -1 rather than 0 to disable the ring so that a zero `Options`
keeps the old behavior.

## Time control

`runner.Pause()`, `Resume()` and `Paused()` stop and restart ticking without
stopping the loop; paused time is not caught up. `runner.SetSpeed(x)` runs `x`
ticks per tick period (`0.5` every other period, `4` four per wake, `0` stops).
Speeds above 1 run several ticks per wake, bounded by `CatchUpBudget`. For
very large time ratios, keep the tick rate modest and use coarse steps with
`game.Clock` scaling instead (see the game README). `TickEvent.Behind` and
`TickEvent.Lag` show when the runner cannot keep up.

## Features

- **Fixed-rate tick loop** — deterministic simulation at configurable tick rate
- **Input collection** — hub "input" events routed to simulation
- **State broadcast** — "sim:tick" events with frame number + serialized state
- **JSON state encoding** — valid JSON state can be embedded directly instead of base64-encoded
- **Snapshot ring** — 128-frame history for rollback support (configurable)
- **Replay recording** — full input log for match replay
- **Spectator sync** — joining clients receive current snapshot
