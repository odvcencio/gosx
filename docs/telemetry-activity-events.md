# Activity events

Activity events use the kind's declared names and typed event codec. Unknown names map to `other`. Validated fields are copied into an immutable event record; the framework retains no application value or per-activity event history.

Events share one count and byte bounded FIFO on the existing worker. The default limits are 4,096 records and 4 MiB, including the backing record headers and any payload being processed. A full queue rejects immediately with `ErrQueueFull`. Worker completion releases both the count and payload reservation, and clears the retained slot.

The default activity cap is 1,024 accepted events. Cap, queue and validation failures leave accepted totals unchanged and count rejected attempts while the activity remains open. Checkpoints and finals include the logical accepted and dropped totals; a failed sink does not erase an accepted event. A concurrent conflicting projection can return `ErrConflict`. Events after the frozen final return `ErrClosed`.

The field budget applies to activity, participant and current event fields together. Event data cannot consume the space already reserved for framework final fields. Encoders run outside entity locks, and failed encodings cannot replace the activity projection.

Memory processing has no file, logger or historical list. The sink worker and durable persistence follow separately. Use `Activity.Event` after Begin.
