# Telemetry aggregate adapters

Operation metrics attach to the existing App operation observer. Declare
application pairs in `Metrics.Operations` before `telemetry.Enable`; the fixed
ISR pairs are included. Unknown pairs aggregate under `other/other`, and status
uses `ok`, `error`, `cancelled`, `timeout` or `other`. Target paths and error text
are discarded.

Auth metrics require explicit attachment to the application's auth manager:

```go
manager.UseObserver(tel.AuthObserver())
```

Declare types and providers in `Metrics.AuthTypes` and `Metrics.AuthProviders`
before Enable. The built-in types are `sign_in` and `sign_out`. Unknown values
aggregate under `other`. Identity, email, path, method and error text are not
read by the telemetry observer. Existing auth observers remain independent.

Declare dependency names in `Metrics.DegradedComponents`, then call
`tel.SetDegraded("database", true)` when that dependency fails and `false` after
recovery. The framework names `telemetry`, `persistence`, `upload` and `clock`
are included. Undeclared names fail without adding labels. Degradation does
not change public readiness.

The registry reserves hub, loop and activity families before the server-owned
catalog admits routes. Register kinds before App.Build. Each page and its
derived error row fit together or collapse together into `(other)`. An error
row shares the page duration histogram and reserves response counters for
statuses an error renderer can produce. The fixed public, runtime, ISR,
unmatched, other and hub rows also retain their capacity.

GET routes reserve GET and HEAD. Wildcard routes reserve all eight finite
method classes. Route admission is limited by both scalar sample lines and
retained bytes; reaching either cap collapses excess whole routes. Concrete
request paths and unregistered patterns cannot add series. Published lookup
tables are immutable, and warm aggregation allocates no memory.

This delivery prerequisite provides the inventory and operation, auth and
degradation adapters. Request response measurement, cached runtime/readiness/
scheduler collection and sanitized client ingress attach in subsequent owner
slices. Enable continues to reject unavailable configured features with the
fixed `unsupported` class. The internal listener is a separate slice.
