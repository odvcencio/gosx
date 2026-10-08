# Activity participants

An activity participant represents a bounded seat, with a random activity-scoped ID and declared domain fields. Seat indices run from zero to the configured participant limit minus one. A spectator uses the separate `-1` slot. Duplicate occupied seats reject; update the existing participant's declared fields for a bot takeover.

Participant projections follow the activity's transaction rules. The encoder runs outside the activity lock, and a concurrent revision returns `ErrConflict`. Failed validation preserves the previous fields. Snapshots copy participant fields and links. Mutations after the frozen final return `ErrClosed`.

Each participant has one active opaque connection reference. Repeating the same join is idempotent. A new reference counts a reconnect, including replacement before the prior leave arrives. A leave applies only to the currently active reference; stale and duplicate leaves do not change totals. Presence duration uses monotonic server time and freezes with the activity final.

Connection references belong to one telemetry owner. Bot participants and unconsented references do not persist client or session details. Consented human associations retain at most 16 historical session links. Optional oldest links are removed when the reserved final projection needs room, setting `links_truncated` while preserving join, leave and reconnect totals. Activities remain able to finalize as these framework fields grow.

This prerequisite retains internal activity construction until the event owner completes the required public API. The framework session association source follows in the privacy and browser slices; there is no application constructor accepting arbitrary session IDs.
