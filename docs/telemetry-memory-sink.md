# Memory sink

`telemetrytest.NewMemorySink(maxRecords, maxBytes)` creates a synchronous,
portable sink for deterministic lifecycle tests and live-only acceptance.
Zero capacities use 128 records and 1 MiB; negative capacities fail. A zero
`MemorySink` uses the same defaults. The helper imports portable leaf types
and satisfies `telemetry.Sink` without importing the root telemetry package.

Both limits apply together. Byte admission accounts for encoded records,
copied payloads and the fixed backing array of record headers. Capacity is
reserved before publication, and concurrent writes cannot grow past it.
Full admission returns `ErrQueueFull`; a zero Record is invalid.

`Records()` returns a copied slice of immutable records. Records remain
readable after Close, and copied payload views belong to the caller's own
memory budget. Successful Close is terminal and idempotent. Subsequent writes
return `ErrClosed`.

`FailNext("write", err)`, `"flush"` and `"close"` inject one failure for that
operation. Other operation names fail with a fixed class. Cancellation does
not consume an injection. Returned errors preserve the injected identity
under `errors.Is` and expose fixed text instead of arbitrary error details.
Each operation requires a nonnil context and returns on cancellation.

Memory writes acknowledge live acceptance. They do not prove crash durability.
The framework activity and sink-worker slices supply receipts and ordered
dispatch; this helper does not start a goroutine or create an implicit sink.
