# Go-WASM hub client

`hubclient.Dial(path, options, handler)` opens a framework-owned browser
WebSocket for a GoSX Go-WASM engine. Its host transport forwards raw text to Go
once. The Go client decodes the standard `{ "event": ..., "data": ... }`
envelope and leaves `data` as `json.RawMessage` for application-specific
decoding. No page-authored JavaScript or DOM `CustomEvent` is involved.

`Client.Send` encodes that same envelope, and `ReadyState`, `Close`, and
`Dispose` manage the browser socket. `Reconnect` replaces the transport using
the caller's new path; the package deliberately does not set retry timers,
replay input, or decide when an application session is valid. Call `Dispose`
from the engine handle's `Dispose` method. Both inbound and outbound text are
bounded by `Options.MaxFrameBytes` (default and maximum: 8 MiB). Binary frames
are ignored by this text protocol client.

This API is separate from `PageRuntime.BindHub`: that declarative path binds
events to shared signals and has its own reconnect policy. Use this client for
WASM engines that need an application handshake, a dynamic room URL, or close
code/reason handling.
