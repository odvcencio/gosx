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

## Go callbacks and calls into JavaScript

Some browser calls run callbacks before they return. `dispatchEvent`, `focus`,
`click` and `AbortController.abort` run listeners, `new Promise(executor)` runs
its executor, and a write to the shared signal store runs every subscriber.
When the callback is a Go function made with `js.FuncOf`, the Go runtime enters
Go while the call from Go into JavaScript is still on the stack. This is a
nested entry.

A nested entry can stop the program, however many goroutines make them. Go's
js/wasm runtime keeps the entries that are in progress on one stack, and each
entry allocates its record before it pushes it. If that allocation pauses the
entry, because it starts the first garbage collection of the process, another
goroutine can run first. A nested entry then goes onto the stack first and the
paused entry on top of it, against the order of the calls on the JavaScript
stack. The runtime resumes the wrong goroutine, and the program stops, usually
with `fatal error: traceback did not unwind completely`. The paused entry can be
any timer, event or promise callback. A program in which a single goroutine made
every nested entry stopped in 19 of 256 runs (`new Promise` with a Go executor)
and 46 of 256 runs (`dispatchEvent` with a Go listener) on go1.26.0, and in 14
and 31 of 256 runs on go1.26.10; the same programs with a native executor or
listener stopped in none. Keeping such calls on one goroutine does not prevent
it. The failure depends on the build and on timing, so a program can pass its
tests and still fail for a user.

GoSX keeps Go out of these calls:

- The waits in `Fetch`, `Context.Navigate`, the `Scene3D` calls and the context
  variants of clipboard and fullscreen do not nest. Their Go handlers only
  signal a channel, and the browser runs them after the call has returned.
  `jsutil.NewPromiseWithResolvers` gives your own code a pending Promise without
  a Go executor.
- Go listeners added with `On`, Go subscribers added with `SubscribeSignal`
  (and so `ServeCalls`), and the netplay link's callbacks are
  `jsutil.Callback`s. When Go itself starts a call that makes the browser run
  them, they only record the call, and Go runs them after the JavaScript call
  has returned and before the Go function returns. These are the calls:
  `Element.Dispatch`, `Focus`, `Blur` and `Click`, `DispatchResize`,
  `DispatchError`, `Context.Emit`, `Context.Navigate` (the page's `navigate`
  dispatches `gosx:navigation:state` before it first waits, and a same-document
  navigation also dispatches `gosx:navigate`), `Context.SetSignal`,
  `SetSignals`, `SetChangedSignals`, the main-thread batches of a `View`, the
  replay of the current value that `ServeCalls` asks for when it subscribes, and
  the netplay link's `Open` and `Signal`.

What this changes for your code:

- A Go listener of a dispatch that Go starts runs after the browser has
  delivered the event to every listener, page scripts included, in the order the
  browser called them. `PreventDefault`, `StopPropagation` and
  `StopImmediatePropagation` come too late to change that dispatch: a Go
  listener cannot cancel the click that `Click` performs. `Event.CurrentTarget`
  still names the element the listener is attached to. An event that the page
  raises, such as a user's click, still reaches a Go listener inside the
  browser's own dispatch, and the listener can cancel it.
- The same holds for every event that the browser or page code raises while Go
  is inside one of these calls, not only for the event that Go dispatches. A Go
  listener cannot cancel the default action of such an event. `Click` on a
  submit button submits its form, whatever a Go `submit` listener does, and so
  does a `requestSubmit` that a page script makes in reaction to `SetSignal` or
  `Emit`. In Chrome for Testing 156, a form whose Go `submit` listener called
  `PreventDefault` loaded its target frame once after `Click()` on its submit
  button and once after a `requestSubmit` made during `SetSignal`; the same
  `requestSubmit` started by a page timer loaded it zero times, because the Go
  listener cancelled it. To let Go code decide whether a form submits, call that
  code directly instead of calling `Click`.
- When the target is inside a shadow tree, `Event.Target` and
  `Event.RelatedTarget` do not name what they named during the dispatch. After
  the dispatch the browser leaves them as seen from outside the shadow tree,
  which is the outermost shadow host for a composed event (a click or a focus
  change), and clears them for an event that is not composed, such as every
  event that `Element.Dispatch` creates. A cleared one is an invalid `Element`.
  `Event.CurrentTarget` still names the element the listener is attached to.
- On the main thread, a Go subscriber of a shared signal that Go writes runs
  after the page's other subscribers of that write, and sees the whole batch. A
  subscriber that writes another signal has that write's subscribers finish
  before its own write returns, as before. In a worker, a write goes to the page
  as a message, and Go subscribers run later, when the page sends the value
  back.
- A Go listener that an earlier Go listener disposes during the same dispatch
  does not run, as in the browser. Go listeners that dispatch another event see
  that event's listeners finish before they continue.

Use `jsutil.NewCallback` where you would use `js.FuncOf` for a listener,
subscriber or notification that JavaScript may call while Go is inside a call,
and make that call with `jsutil.Call` instead of `js.Value.Call`. A function made
with `js.FuncOf`, and a `dispatchEvent`, `focus` or `click` made with
`js.Value.Call`, still nest when Go listeners are attached. Where you can, call
the Go consumers directly and dispatch the DOM event only for page scripts.

`jsutil.DocumentContext` does not nest either. The browser aborts the document
signal inside the call that starts a navigation, and Go starts one with
`Context.Navigate` or with a `location.assign` call through `syscall/js`. The
context listens with a native function and cancels from a goroutine. It ends a
few microtasks after the abort, once the call that started the navigation has
returned, and not inside that call. So after Go starts a navigation, a request
that Go starts before the program next returns to JavaScript passes the
`Fetch` check of the context and goes out; the browser aborts it a moment later.
To refuse such a request, call `DocumentContext` again right before it: it reads
the signal at once, so a call made after the abort returns a context that is
already canceled.

Go's standard library nests in a browser too. A write to `os.Stdout` or
`os.Stderr`, which includes `fmt.Println` and the `log` package, makes
`wasm_exec.js` call a Go callback before its `fs.write` returns. In Chrome for
Testing 156 with the `wasm_exec.js` of go1.26.0, each write was one nested entry;
Node's host calls the callback later and shows none. GoSX cannot change that.
`browser.LogError` calls `console.error` without a Go callback.

Not covered: the island runtime built with TinyGo has its own scheduler. The
evidence above is for the standard Go toolchain, and the focus and click host
operations of the TinyGo runtime were not tested. The `examples/*-spike`
programs keep their own Go Promise executors.

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
