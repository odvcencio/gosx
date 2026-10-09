package sim

import "time"

// TickEvent describes one completed tick, including input drain, simulation,
// snapshot, replay, state serialization and broadcast encode/enqueue. StateBytes
// is measured before hub envelope encoding; Inputs counts drained players.
type TickEvent struct {
	Frame              uint64
	Duration, Lag      time.Duration
	StateBytes, Inputs int
}

// Observer runs synchronously after a tick, outside runner state locks. It must
// return promptly; its own work is excluded from Duration. There is one event
// per executed tick, even when the ticker drops deadlines during slow work.
type Observer interface{ ObserveTick(TickEvent) }
