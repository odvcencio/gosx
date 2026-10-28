# Browser services for Go/WASM engines

Application engines can use typed GoSX packages instead of importing
`syscall/js` or reaching into renderer handles. Browser operations remain
implemented by the framework's Go/WASM host adapters and TypeScript runtime.
These packages do not replace application rules, simulation or wire protocols.

| Concern | Package and entry points | Ownership |
| --- | --- | --- |
| DOM and input | `client/browser`, `engine/wasm.Context.MountElement()` | Elements are live handles; dispose listeners explicitly. |
| Frame loop and gamepads | `game/host` | Stop the frame clock and release subscriptions on unmount. |
| Timers and attributes | `client/browser.Timeout`, `Interval`, `ObserveAttributes` | Stop timers and dispose observers on unmount. |
| HTTP | `client/browser.Fetch` | Pass a component context; cancellation aborts the request. |
| Storage | `game/storage.Local`, `Session` | `Read` distinguishes a missing key from denied access; `Keys` is a bounded snapshot. |
| WebSocket | `hub/socket.Dial` | Call `Dispose` on replacement/unmount; copy borrowed binary bytes before retention. |
| Hub protocol | `hub/client` | Uses the same socket transport; retains the Hub protocol and reconnect behavior. |
| Audio | `game/audio/host` | Close the host; stop/disconnect owned voices and graphs when their lifetimes end. |
| Retained graphics | `scene.NewSurface`, `scene.NewStreamWriter` | Dispose producer handles; the Scene3D mount retains renderer ownership. |
| Native capabilities | `client/browser` clipboard/fullscreen/desktop APIs | Start user-gesture operations synchronously and use context variants for pending completion. |

## DOM and event cost

`browser.Element` is an opaque value, without a public raw JavaScript escape.
`Element.On` returns an owned listener; an event reads individual fields only
when requested. Obtaining `Event.Target()` does not enumerate mouse, touch,
keyboard or controller data. Keep handles for stable DOM and suppress repeated
writes in the presentation layer when a value has not changed.

`browser.Markup` represents authored markup, not untrusted user text. Use
`SetText` for arbitrary strings. Replacing markup changes descendant identity;
discard cached handles to those descendants before the next update.

## Asynchronous lifetimes

Create a `context.WithCancel` for the mounted engine and cancel it in `Dispose`.
Use that context for `Fetch`, context variants of clipboard/fullscreen and
`Desktop().WithContext(ctx)`. DOM listeners, timers, observers, socket callbacks
and audio nodes also have explicit owners. A callback already executing may
dispose its owner safely. Application callbacks must still check their own
state after an asynchronous completion before mutating UI.

Timers and observers clear application closures even when a custom host throws
during cancellation. Disposal can retry native cancellation. If a malformed
host constructor retains a callback and then throws without returning its
native handle, GoSX keeps an inert handler rather than releasing a function
that the host might call later; no application callback remains attached.

`GuardRequests` is an optional path-based request policy. The server page opts
in with `ctx.Runtime().RequireFeature("browser-services")`; the feature loader
installs that service before mounting engines. Other runtime/lite pages do not
fetch it. Its hot forwarding path stays in JavaScript and preserves native
arguments and receivers. Dispose the guard to remove only its own policy.
Telemetry opt-out drops queued events and prevents unload beacons.

## Calls that run Go listeners inside the call

A few browser calls run listeners before they return. `Element.Dispatch`,
`Focus`, `Blur` and `Click`, `DispatchResize` and `DispatchError` do. On the
main thread, a write to the shared signal store (`Context.SetSignal` and
`SetSignals`) calls every subscriber before it returns, including Go
subscribers from `SubscribeSignal`. A Go listener then runs inside the call
that caused it.

One goroutine can do this safely. Go's js/wasm runtime keeps these nested
entries on one stack, and a program can stop with `fatal error: traceback did
not unwind completely` when two goroutines are inside such calls at once. Make
these calls from one goroutine, or call the Go consumers directly and dispatch
the DOM event only for page scripts.

The waits in `Fetch`, `Context.Navigate`, the `Scene3D` calls and the context
variants of clipboard and fullscreen do not nest. Their Go handlers only signal
a channel, and the browser runs them after the call has returned, so any
goroutine can wait this way. Page code that these calls start can still
dispatch events that reach Go listeners. `jsutil.NewPromiseWithResolvers` gives
your own code a pending Promise without a Go executor.

## Retained instance streaming

Mount geometry and materials with ordinary typed scene commands. A
`scene.StreamWriter` updates an existing primitive batch. Call `Prepare`
before a group of writes; it finds the current renderer handle and loads the
shared codec if needed. Until ready, keep the normal declaration fallback.

`Apply` encodes into retained Go storage and copies into retained JavaScript
storage. The renderer consumes the view synchronously; a subsequent call may
overwrite it safely. `Apply` performs no DOM lookup or JSON conversion.
It rejects reentrant writes, which could otherwise corrupt an active view.
For skinned animation, use the existing typed `PoseFrame`/`MotionFrame` paths.
To pose a skinned model from your own simulation clock, see
[sampled clip frames](scene3d-clip-frames.md).

`InstanceStreamFrame.Encode()` returns an independently owned byte slice.
`EncodeInto(dst)` may alias `dst`; its bytes are valid until that storage is
reused. The shared codec can be preloaded without invisible warm-up actors.

Use `InstanceStreamTransform` for a fixed count of matrices. For changing
counts and colors, pack an `InstanceStreamTransformColor` snapshot in Go:

```go
frame := scene.InstanceStreamFrame{BatchID: "sparks", Revision: nextRevision}
frame.SetTransformColors([]scene.InstanceTransformColor{{
    Transform: [16]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 2, 3, 4, 1},
    Color: [4]float32{1, .25, 0, 1}, // normalized RGBA, multiplied by material
}})
applied, err := writer.Apply(frame)
```

Retain the frame to reuse its packing storage. Matrices are local to the
declared parent; colors are finite normalized RGBA floats, as on the numeric
runtime color path. Use CSS channel values divided by 255; for `#808080`, each RGB channel is `128.0/255`.
The snapshot publishes count, transforms and colors together; source arrays
may be reused when `Apply` returns. A zero count hides the instances while
keeping the declaration available for re-growth. Indices remain positional.
Geometry, materials, parent and custom attributes remain declared; changes
to those fields, including attributes tied to new membership, require a
normal declaration. A removed batch rejects streams. Re-declaring batches
starts a new revision lifetime and drops frames queued during codec loading.
Until `Prepare` succeeds or when `Apply` returns false, retain the normal
declaration fallback. Skinned models continue to use `PoseFrame`.

## Build only the application module

Configure standard Go/WASM packages in `gosx.config.json`:

```json
{"build":{"goWASM":{"client":"./client"}}}
```

```sh
gosx build --prod --go-wasm-only --output dist/go-wasm .
```

This emits a manifest, hashed WASM with Brotli/gzip siblings, and a loader from
the project's standard Go toolchain. It runs no hooks, component discovery,
server build or runtime compiler. Consume the manifest instead of guessing
hashed paths, and use the matching loader. Full `gosx build --prod` includes
the same module pipeline alongside the rest of the application build.
