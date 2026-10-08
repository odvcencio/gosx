# Coordinated shutdown

Register resource owners with `App.UseShutdownSource` or
`App.UseShutdownHook` before the first build. Mounting a handler does not
register its owner. `Signal` stops admission or cancels work without waiting
or performing I/O. `Drain` and `Flush` must respect their context.

`App.Shutdown` is terminal. It drains its HTTP server before stopping scheduled
work, then signals resource owners, calls their drains in registration order,
and calls flushes in reverse order. An externally hosted HTTP server must be
drained before calling `App.Shutdown`. Hijacked WebSockets need an explicit
owner drain.

Without a deadline, scheduled tasks get 30 seconds to finish, then are
cancelled with a cause matching `context.Canceled`. Shutdown waits without
limit for those cancelled runs before calling the hooks.

With a deadline, the reserve is the greater of 5 seconds and 25% of the time
remaining after HTTP drains. Tasks are cancelled at the earlier of the
configured `ShutdownGrace` boundary and the deadline minus that reserve.
Shutdown waits for cancelled runs only until the deadline minus half the
smaller of that reserve and the remaining time, then calls Drain and Flush
with the remaining time. A task that
ignores cancellation can therefore overlap those hooks; its scheduler owner
retains it until it returns. Hooks must account for that possibility when
closing resources shared with scheduled tasks. An unfinished scheduler returns
an error matching `context.DeadlineExceeded` even when hooks finish in time.
When fewer than five seconds remain, cancellation is immediate and the join
uses at most half the remaining time, leaving the other half for hooks. This
budget grows continuously as the deadline increases. With at least five
seconds remaining, hooks retain half the reserve after scheduler joining.

Concurrent shutdown callers share one pipeline and wait only until their own
context expires. Hook errors retain their original identity for `errors.Is`
and `errors.As`, while printed lifecycle errors contain fixed phase names.
