# Hub observers

Install subscribers with `Hub.UseObserver` before the first connection upgrade.
Subscriptions are additive. Embed `hub.NoopObserver` and implement the
callbacks you need, so future callbacks do not require changes to your type.

Callbacks run synchronously and may run concurrently, outside hub and client
locks. Keep them bounded; do not perform I/O or wait for pump shutdown. A
panicking subscriber is detached, and other subscribers receive
`ObserverPanicked` without the panic text.

Connection callbacks report registration and cleanup. `Rejected` reports
connection admission failures; `MessageRejected` reports malformed or
rate-limited payloads on accepted connections. `Message` reports logical
payload bytes and sampled queue/drop events. `Message` with `Dropped=true`
accounts for every queue loss; `Broadcast` summarizes those same losses and
must not be added again. `Count` preserves coalesced multiplicity (zero means
one); such callbacks may have a nil client. Accepted queue samples describe
depth before the enqueue, including zero. Broadcast, handler, control RTT,
RTT timeout and `Closed` callbacks are active. `ClientAssociated` is reserved
for browser association integration.

`Hub.Close(ctx)` stops admission and waits for reservations, both pumps and
admitted observer callbacks, including `Send`, latch replay and CRDT enqueue
callbacks on application goroutines. `Closed` is the final callback for each active
subscriber, even after `Close` returns. An expired caller leaves the shared
close owner running until cleanup completes.

Release any lock an observer callback may acquire before calling `Hub.Close`.
Holding it can deadlock: `Close` waits for the callback while the callback waits
for the caller's lock. Callbacks must also never wait for the same hub to close.
Stop admission under an application lock if needed, release the lock, then
drain the hub. Use `SignalShutdown` to start closing without waiting and
`Drain(ctx)` to wait for that same owner.
