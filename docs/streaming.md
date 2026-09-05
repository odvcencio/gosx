# Streaming: cancellation, errors, and cache ownership

`ctx.Defer` and `ctx.Suspense` still accept `func() (gosx.Node, error)`.
Their fallback is rendered in the initial shell; admitted fragments are sent
as they finish, before the document tail. Use the additive context-aware forms
for database, HTTP, or other work that can honor cancellation:

```go
func profile(ctx *server.Context) gosx.Node {
    if err := ctx.SetStreamOptions(server.StreamOptions{
        Concurrency: 4,
        Timeout:     5 * time.Second,
        OnError: func(event server.StreamError) {
            // Internal diagnostics only; never return event.Err to a browser.
            log.Printf("stream request=%q slot=%q operation=%q error=%q",
                event.RequestID, event.SlotID, event.Operation, event.Err)
        },
    }); err != nil {
        panic(err) // Invalid application configuration, before the shell.
    }
    return ctx.SuspenseContext(gosx.Text("Loading profile…"),
        func(work context.Context) (gosx.Node, error) {
            name, err := loadProfileName(work)
            if err != nil {
                return gosx.Node{}, err
            }
            return gosx.Text(name), nil
        })
}
```

This handler sketch uses `context`, `log`, `time`, `m31labs.dev/gosx`, and
`m31labs.dev/gosx/server`; `loadProfileName` is application-owned. The same
methods are available through `PageState`, file-route contexts, and a standalone
`DeferredRegistry`. `DeferContextWithOptions` and
`SuspenseContextWithOptions` retain `DeferredOptions` for placeholder IDs,
tags, classes, and boundary labels. Configure/register before rendering; do not
mutate the registry, page metadata, session, headers, or response from a resolver.

## Bounded work and cooperative cancellation

Zero-valued `StreamOptions` select **8 concurrent callbacks per response** and
a **30-second stream timeout**. Explicit concurrency must be 1–128; a supplied
timeout must be positive. Invalid settings return an error without replacing
the previous settings. The timeout starts after shell rendering, before shell
output, and includes waiting for callback admission and writing deferred output.
It is not a deadline for the initial page handler, layout, or shell rendering.

A shared process ceiling of **128 active callbacks** also applies. Queued
sections do not get their own goroutines. Completed callbacks release their
permit; an abandoned noncooperating callback keeps its permit until it returns.
Admission waits cancelably, so abandoned requests cannot continually launch
unaccounted replacement work. This is a concurrency bound, not a memory/CPU
quota or a fairness guarantee. Application callbacks can still allocate large
results or start their own goroutines, and registered placeholders remain
proportional to the page's section count.
Do not make a callback wait for another deferred callback to be admitted:
bounded scheduling can otherwise leave that dependency queued until timeout.

The resolver context ends on request cancellation, timeout, or detected
write/flush failure. The framework stops admission and returns from waiting
without waiting for an uncooperative callback. Results that arrive after that
are discarded; workers never receive the response writer. Go cannot forcibly
stop arbitrary Go callbacks. Permanently blocked callbacks can exhaust the
shared ceiling, causing later streams to time out waiting for admission.
Pass the provided context into downstream I/O and make long computation loops
check it; do not capture a page context for response/session mutations.

Only the response goroutine writes HTML and observes errors. Standard `net/http`
writers support write deadlines: cancellation expires that deadline to unblock
output and prevent an incomplete streamed body from being finalized as a
complete HTTP response. Plain and gzip HTTP/1.1 tests require an incomplete-body
error and a new connection after a timeout. The framework's interception,
observation, gzip, and session wrappers preserve flush errors and allow
`http.ResponseController` to reach underlying deadlines; session cookies still
commit before body output.

Custom wrappers must expose `FlushError() error` when flushing can fail and
`Unwrap() http.ResponseWriter` or equivalent deadline support. A legacy writer
without flushing is supported for successful buffered output, but cannot claim
incremental delivery. An arbitrary writer that blocks forever and ignores
deadlines cannot be preempted safely; neither its timeout nor transport-abort
behavior is guaranteed. Likewise an `OnError` observer must return promptly and
must not write the response. There is no orphan writer goroutine to finish
output after the handler returns.

## Public failures and server diagnostics

Returned errors normally render only “The server could not finish this
section.” An error may intentionally opt into public text through the existing
structural contract `SafeMessage() string`; wrapping with `%w` preserves that
opt-in. The message is escaped as text, not treated as markup. An empty safe
message falls back to the neutral message. Do not mark private/provider details
safe. Every panic—including a panic carrying a `SafeMessage` error—renders the
neutral message. Panics from resolving or rendering a fragment are contained.

`StreamOptions.OnError` receives the internal error, request ID, slot ID, and
operation on the response goroutine. Nil uses the standard logger. GoSX does
not collect request bodies, URLs, queries, or headers for this diagnostic;
application-provided error strings can still contain sensitive information and
must be handled as private server logs. Errors from callbacks completing after
the response ends are discarded with their results, not reported later through
an observer that may capture request state. A timeout/cancellation is reported
for the response itself. Observer panics are logged and do not expose details
to the browser.

## Validators, shared responses, and ISR

The full bytes of a stream are unknown when its headers commit. GoSX therefore
does **not** generate a request-hash or shell-hash ETag for streamed responses.
Unchanged and changed fragments both render normally unless the application
supplies a valid explicit validator. This does not disable an explicitly chosen
public/private cache policy: select a policy appropriate for every fragment.

`SetETag` and `SetLastModified` remain caller-owned resource-version contracts.
Only use them when the version covers the shell **and all deferred content**.
Matching explicit validators may return 304 without launching any callbacks;
`If-None-Match` takes precedence over `If-Modified-Since`, even on a mismatch.
HEAD renders headers but launches no deferred work. Ordinary non-streamed pages
retain content-derived ETags. At the lower level,
`ApplyCacheHeadersForBody(..., nil)` means an unknown representation, while a
non-nil empty slice means a known empty body. Direct legacy
`ApplyCacheHeaders` without a reported body remains a request/version-based
API, not a content validator.

Private chunks use the response's CSP nonce. Shared-cacheable shells and chunks
strip that exact per-request nonce and retain the existing nonce-free,
fail-closed shared CSP; a public cache is not permission to weaken CSP. The
existing native fragment replacement fallback remains available when the
managed browser runtime is absent.

ISR refreshes track stream failures independently of status 200 and independently
of the error observer. Timeout, cancellation, write/flush failure, a returned
resolver error (even a safe-message error), or a resolver/rendering panic prevents
publication and leaves the last healthy artifact and its state intact. A
completed healthy stream can still publish. Stored ISR responses retain their
existing artifact-modification-time `Last-Modified`/`http.ServeContent` semantics;
this change does not add ISR ETags or redesign storage/revalidation.
