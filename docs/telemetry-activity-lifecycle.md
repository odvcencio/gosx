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

## Shutdown ownership audit

Normal Close, worker clock failure, close clock failure and an expired shared
deadline all reach the same unconditional worker cleanup before `done` closes.
Cleanup disables admission before releasing live contributions. It does not
read the clock or invoke application codecs. Close keeps its original error.

| Holder | Release path in every exit case | Exit assertion |
| --- | --- | --- |
| Activity live and loop-attachment maps; reserved 32 KiB slots | `collectActivityReceipts` releases accepted finals; `releaseUnfinishedActivities` removes the remainder | Both maps empty; slot bytes at declaration baseline |
| Per-kind open count and gauge | Final acceptance updates finish metrics; unfinished cleanup removes abandoned contributions | Counts and gauges zero |
| Participant presence map, connection token, owner ref and elapsed-presence staging | Final collection clears `activityEntity.presence` after history is copied; unfinished cleanup clears it after releasing the owner lock | Presence maps empty; late participant callbacks return `ErrClosed` |
| Completed parent ID ring and its cursor | `releaseUnfinishedActivities` clears `known` and `nextKnown` after final collection | Ring empty; cursor zero |
| Event queue payloads, slot records, queued and in-flight counts | Stopped activity admission followed by `drainActivityEvents`, synchronized with `eventPublish`; each take clears its slot and each release removes its payload charge | Empty records; zero queued/in-flight; bytes equal reserved queue headers |
| Accepted checkpoint and final receipts | Checkpoints complete synchronously; final collection completes admitted receipts before worker completion | Every accepted receipt ready; unchanged memory result |
| Activity/participant fields, session links, reduced client data, event totals and final loop health | Owner maps release entity references; caller-held immutable projections preserve copied history | Final projection unchanged after transient connection cleanup |
| Transaction and field-pool leases, cells and scratch | Application operation and encoder defers, independent of worker completion | Completed operations leave zero leases and cleared scratch |
| Loop slots, pending tokens, health bins, instance counts and gauges | `releaseLoops` | Empty slots and health; zero instance contributions |
| Hub subscription map, subscriber slot, detach callback, source pointer, client/instance counts and gauges | `releaseHubs` retires subscriptions and releases attachment reservations | Empty map; detached subscriber; zero contributions; source observer slot reusable |
| HTTP in-flight count and gauge | Disabled admission followed by `releaseRequests`, serialized with `requestDelta` | Both zero; late start/completion callbacks cannot restore them |
| Memory and series gauges | `updateCoreUsage` after activity, event, loop, hub and request cleanup | Remaining fixed reservations accounted; metric reservations unchanged |
| Worker wake notifications and ticker | Notification publishers share the owner mutex; cleanup drains `wake`, stops the ticker and closes `done` | Wake empty; no ticker; disabled admission; completion published |

The event ring's backing headers remain a fixed reservation, including when it
contains no payload. The default 4,096 headers currently reserve 786,432 bytes.
The exit audit derives that reservation from the queue capacity and record
header size rather than treating it as abandoned payload storage.

Finite kind/codec declarations, metric tuples and historical aggregates, field
pool capacity, loop arenas, request catalog tables and worker synchronization
metadata remain owned by the retained Telemetry instance. Response writers and
active HTTP requests remain application-owned. Readiness snapshots belong to
the App's bounded completed-probe cache; telemetry cleanup neither reruns nor
clears readiness checks. A synchronous encoder still executing at worker exit
releases its bounded staging lease when its own call returns.

`TestActivityWorkerExitReleasesOwnedState` exercises the four exit cases with
completed parent correlation, two unfinished activities, an accepted final,
active participants, queued events, outstanding HTTP observations, loop health,
hub subscriptions and forbidden application codecs. It checks released state,
fixed reservations, late callbacks and preserved final projections.
