# Typed activities

Register a finite activity kind before `App.Build`, then begin a match or round with its declared dimensions and typed domain fields. The activity, participant and event codecs have separate Go types, so a seat projection cannot be passed as a match update.

```go
kind, err := telemetry.NewActivityKind[MatchFields, SeatFields, RoundFields](
    tel, "match", kindOptions, codecs)
if err != nil { return err }
match, err := kind.Begin(telemetry.ActivityStart[MatchFields]{Fields: initial})
if err != nil { return err }
```

Declare every codec field, enum and array bound. Encoders run outside entity locks and must not capture game or request state. `Set` replaces the previous projection; a failed encoding preserves it. Concurrent conflicting mutations return `ErrConflict`. Unknown finite dimension combinations, outcomes, reasons and event names map to the fixed `other` values.

`End` freezes fields, monotonic duration, loop health and event totals once. An identical normalized final reuses the same receipt; a different final returns `ErrConflict`. Wait with your own context to acknowledge that operation without cancelling shared work. `PersistenceMemory` confirms live state only and makes no crash-durability promise. Durable sinks follow in the persistence slices.

Begin reserves the bounded live slot and final capacity before returning. A pending final keeps that slot until acknowledgement. Snapshot views copy fields, participants and links; callers retaining a terminal view pay for their own copy. The framework keeps no historical record list when persistence is off.

A loop instance can belong to one open activity. A parent ID may refer to a live activity or one of the most recent 256 acknowledged local activity IDs. This bounded catalog retains IDs rather than final records. Parent links provide correlation, never authorization.

Participant seats, consented links and event queue budgets are described in the participant and event guides. Nil or disabled handles remain inert. Privacy/browser connection associations follow separately; applications cannot create framework refs from arbitrary session IDs.

This slice enables memory activities. The deferred request, readiness, scheduler, listener and browser features retain their explicit unsupported configuration errors until their owners are delivered.
