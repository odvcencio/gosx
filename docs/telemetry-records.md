# Typed telemetry records

`schema.Record` exposes an immutable record to sinks. `Envelope()` copies its
schema, stream/boot identity, sequence, type, state, UTC observation, entity
revision and CRC-32C. `Activity()`, `Visit()`, `HubSession()`, `ActivityEvent()`
and `ClientHealthSummary()` return copied typed views and a boolean indicating
the matching variant. Mutating a returned slice cannot change the record.

`WriteTo` writes canonical JSONL, and `Bytes` returns a defensive copy. The
envelope uses schema version 1. This version describes the framework format;
applications must migrate older app-owned formats explicitly. A zero Record
is invalid. Constructors are internal to the framework, so a sink cannot
submit arbitrary raw JSON or an unvalidated body.

Records use fixed struct field order, lexicographic domain keys, shortest
finite numbers and UTC timestamps. JSON does not HTML-escape declared enum
values. The CRC-32C covers the canonical envelope and payload with only the
`crc32c` field omitted; the final newline is excluded. It detects corruption,
not deliberate edits. Golden files define the byte contract.

The complete line fits 16 KiB, aggregate domain fields fit 4 KiB, and the copied
record representation fits 32 KiB. IDs are 32 lowercase hexadecimal characters.
Client information contains fixed platform/browser/device families. Optional
tick health is omitted when unavailable; available health explicitly includes
its availability flag and sample count. Percentiles are histogram bin bounds
and can exceed the exact observed maximum within the bin.

The framework assigns sequence and revision through its lifecycle and single
writer. Sequence orders records within a boot; wall time does not order records
across instances. Memory acceptance, local durability and external acceptance
remain separate persistence levels. Sink, activity and persistence owners are
delivered in their subsequent slices.
