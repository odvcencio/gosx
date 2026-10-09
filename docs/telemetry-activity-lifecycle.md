# Activity lifecycle

An activity owns a bounded typed projection, a monotonic start coordinate and
one reserved final slot. Mutations encode outside entity locks, copy validated
fields and commit against the captured revision. Failed encoders, invalid
projections and concurrent conflicts leave the prior record intact.

End freezes the final projection, duration and loop health. Repeating the same
normalized outcome, reason and fields returns the same receipt; a different
final returns ErrConflict. The memory worker acknowledges and releases the
framework's live slot. A retained handle or copied terminal snapshot then
belongs to the caller's memory budget. No historical final list is created.

Receipts identify accepted revisions. Cancelling a caller's Wait stops its own
wait. PersistenceMemory acknowledges live acceptance and cannot prove crash
durability. Checkpoint admits dirty state; clean checkpoints reuse the receipt.
Shutdown stops new admission, lets admitted work finish during source drain,
and interrupts abandoned activities at Flush without running application codecs.
Idle maintenance uses the existing worker and defaults to a six-hour timeout.

If the clock fails or the shared close deadline expires, worker exit releases
unfinished live entries, loop attachments, slot charges and open counts without
constructing a final record. Accepted final receipts still complete successfully.
Caller-held projections stay in the caller's memory budget after owner cleanup.

Framework-owned final health, counts and clock flags are reserved before
publishing a projection. A wall-clock rollback cannot make elapsed duration
negative; final end time is clamped to start and marked clock_adjusted. Health
is frozen with the final and cannot change when a meter later receives ticks.

Register with `telemetry.NewActivityKind`, then call `Begin` before updating
participants or emitting events. Enable supports memory activities. No file
or logger sink is implied; durable persistence follows separately.
