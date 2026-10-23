# Engine workers: run a Go WASM engine off the main thread

A Go WASM engine normally runs on the page's main thread, so a slow frame or a
hidden tab slows the simulation down with it. Set `Thread: engine.ThreadWorker`
and GoSX starts a dedicated Web Worker, loads the engine's Go module inside it,
and keeps the page for what only the page can do: the DOM, Scene3D, shared
signals, input and the `RTCPeerConnection`.

You write Go only for the engine, and for the islands that talk to it. Signals,
calls, views and status (see "Talk to islands") need no JavaScript on the page.
The raw `Port` messages described under "Message protocol" are different: only
page JavaScript can read them, so use shared signals for anything an island must see.

## Turn it on

```go
r.RenderEngine(engine.Config{
    Name:         "Sim",
    Kind:         engine.KindWorker,
    Runtime:      engine.RuntimeGoWASM, // required: only go-wasm engines can run in a worker
    Thread:       engine.ThreadWorker,
    WASMPath:     "/assets/engines/sim.wasm",
    Capabilities: []engine.Capability{engine.CapKeyboard, engine.CapGamepad},
}, gosx.Node{})
```

`Thread` defaults to `engine.ThreadMain`, so existing engines do not change.
`Validate` and `RenderEngine` reject `ThreadWorker` with any other runtime. A
page with a worker engine also loads one extra chunk,
`bootstrap-feature-engine-worker.js`; pages without one load nothing new. The
worker script is that same chunk file, so there is no separate asset to serve.
If your site sets a Content-Security-Policy, allow `worker-src 'self'`.

The engine registers as before. It can ask where it runs:

```go
wasm.Register("Sim", func(ctx wasm.Context) (wasm.Handle, error) {
    if port, ok := ctx.Port(); ok {
        // ctx.Thread() == engine.ThreadWorker: port talks to the page
    }
    ...
})
```

## If the worker cannot start

By default the engine falls back to the main thread and says why. Nothing else
in your Go code changes. Two `engine.Config` fields change that:

| Field | Effect |
|---|---|
| `Worker: engine.WorkerRequired` | The engine never runs on the main thread. If the worker cannot start, the page reports it, the status signal says `unsupported`, and no Go code runs. `WorkerPreferred` (the zero value) is the default behavior above. |
| `ForceWorkerFallback: true` | The page behaves as if the worker could not start (reason `worker-forced-fallback`). Use it in tests to run the same Go code on the main thread, or to see the `unsupported` state under `WorkerRequired`. It is rendered by the server into the manifest, so no page JavaScript is needed. |

Both need `Thread: engine.ThreadWorker`. The JavaScript switch
`window.__gosx_engine_worker_disable = true` still works and gives the reason
`worker-disabled`.

| Reason code | When |
|---|---|
| `worker-unsupported` | The browser has no `Worker`. |
| `worker-disabled` | The page set `window.__gosx_engine_worker_disable = true`. |
| `worker-forced-fallback` | The server set `ForceWorkerFallback` in the engine config. |
| `worker-toolchain-unsupported` | The engine is built with TinyGo. |
| `worker-create-failed` | `new Worker(...)` threw (for example a CSP block). |
| `worker-script-error` | The worker script failed to load or run. |
| `worker-boot-timeout` | The module did not register its factory in time (10 s, or `window.__gosx_go_wasm_factory_timeout_ms`). |
| `worker-fetch-status`, `worker-wasm-exec-missing`, `worker-protocol`, `worker-boot-failed` | The worker reported a boot error or a protocol mismatch. |

A fallback appears as a runtime issue (`__gosx.listIssues()`, type `worker`,
`fallback: "main-thread"`), a telemetry warning, and a
`gosx:engine:thread-fallback` event on `document`. In Go, `ctx.Thread()` is
then `engine.ThreadMain` and `ctx.ThreadFallbackReason()` returns the code. A
`WorkerRequired` engine fires `gosx:engine:worker-unsupported` instead and logs
a runtime issue with severity `error`. A worker that crashes after it mounted is
not restarted: the page reports `worker-crashed`, fires
`gosx:engine:worker-error`, and ends the engine.

## Talk to islands: calls, views and status

An island reaches a worker engine through shared signals. The signal names are
the wire contract; there is no `Port` and no page JavaScript. Three pieces cover
the common needs:

| Need | Engine side (Go) | Island side |
|---|---|---|
| Ask the engine to do something and get the answer | `wasm.ServeCalls` | write `$<channel>`, read `$<channel>.result` |
| Show the engine's state, at a pace the page can follow | `wasm.NewView` | read the signals the view sets |
| Know whether the engine is running | none (the page writes it) | read `$gosx.engine.<engine id>` |

All three work the same when the engine falls back to the main thread, so one
island and one engine serve both cases.

### Wire types

`engine` defines the types. They have no JSON tags, because island records are
keyed by Go field name (a test pins this). An island cannot use a generic type
in a handler, so it declares its own struct with the same field names.

```go
type WorkerStatus struct{ State, Thread, Reason string }
// State: booting | running | unsupported | crashed | disposed
// Thread: "worker", "native" (an engine in the desktop host) or "main" (after a fallback). Reason: a code such as worker-forced-fallback.

type Call[A any] struct { ID uint64; Name string; Args A }
type Result[R any] struct { ID uint64; Name, Error string; Value R; Recent []ResultRef }
type ResultRef struct { ID uint64; Error string }
```

The status signal is named `$gosx.engine.<id>`. The engine ID is
`gosx-engine-<MountID>` when you set `MountID` on the config (also for a worker
engine), so set it to get a stable name. Before the page runs, a status signal
holds the island's default value. Compare against `"running"` rather than
testing for an empty string.

### Calls

```go
// Engine (Go, runs in the worker or on the main thread)
type Add struct{ A, B int }
type Sum struct{ Total int }

off, err := wasm.ServeCalls(ctx, "bv.cmd", wasm.CallOptions{Timeout: 5 * time.Second},
    func(name string, args Add) (Sum, error) {
        if name != "add" {
            return Sum{}, fmt.Errorf("unknown call %q", name)
        }
        return Sum{Total: args.A + args.B}, nil
    })
// return off (a HandleFunc) from your factory, or dispose it in your Handle.
```

```go
// Island (.gsx)
status := signal.NewShared("gosx.engine.gosx-engine-bv", engine.WorkerStatus{})
cmd := signal.NewShared("bv.cmd", Cmd{})            // Cmd has the fields ID, Name, Args
res := signal.NewShared("bv.cmd.result", Reply{})   // Reply has ID, Name, Error, Value, Recent
add := func() { cmd.Set(Cmd{ID: cmd.Get().ID + 1, Name: "add", Args: Args{A: 2, B: 3}}) }
// <button onClick={add} disabled={status.Get().State != "running"}>Add</button>
// <p>{res.Get().Value.Total} {res.Get().Error}</p>
```

The rules:

1. Number calls from 1, increasing by one. The engine ignores any ID at or below
   the last one it handled, because a batch flush can deliver the same value twice.
2. Write the call signal **once per handler**. The runtime batches the writes of
   one handler, so two writes send only the last.
3. A result with `ID >=` your call's ID means your call is finished. If the IDs
   are equal, `Value` and `Error` are its reply. If the result is newer, find
   your ID in `Recent` (the last `CallOptions.Recent` replies, default 8; it holds
   the ID and error, not the value).
4. A call whose arguments do not decode gets `Error: "bad-args: ..."`. A handler
   that returns an error or panics gets that message in `Error`.
5. A call whose ID decodes but whose other fields do not (for example a number
   for `Name`) gets `Error: "bad-call: ..."`. A call with no usable ID cannot be
   answered.
6. A call written before the engine subscribed is handled from the signal's
   current value, unless the result signal shows it was already answered.
   Shared signals outlive an engine, so an engine that remounts does not run an
   old call again. The replay runs before `ServeCalls` returns on the main
   thread, and a little later, as a message, in a worker.

On a worker engine the page also watches each call. If no reply reaches the
result signal within `CallOptions.Timeout` (default 10 s), the page writes
`Error: "timeout"`. If the worker dies, every pending call, and any later one,
gets `Error: "worker-crashed"`; this holds for a crash while the engine mounts
too. A worker that is only disposed (or whose mount is cancelled) fails nothing:
the page just stops watching. The page enforces the timeout, so it works while
the worker is stuck in a long loop. If the worker finishes the call late, its real
reply replaces the timeout. On the main thread there is no watchdog; a handler
that blocks blocks the page.

A handler must not block on an asynchronous JavaScript result, for example
`http.Get` or a channel that only a JavaScript callback fills. The handler runs
inside a JavaScript callback, and Go cannot run other callbacks until it
returns, so the engine deadlocks. Start a goroutine for slow work and answer
with a later call, or have the island poll a signal.

Any script on the page can write the call, result and status signals, not only
the engine and the page host. Treat all three as untrusted input.

### Views

`wasm.NewView` sends the engine's state to the page at a pace the page can
follow, so a simulation can call `Set` every tick:

```go
view := wasm.NewView(ctx, wasm.ViewOptions{MinInterval: 100 * time.Millisecond}) // 10 Hz, the default
view.Set(map[string]any{"$bv.hud": hud, "$bv.crew": crew}) // newest value of each signal wins
// view.Dispose() in your Handle.
```

A batch goes out when at least `MinInterval` has passed and the page has
applied the previous batch (the page acknowledges each one; an acknowledgement
that never arrives frees the credit after `AckTimeout`, 2 s). A batch the page
rejects, or that fails to send, goes back into the queue (a newer value for the
same signal wins) and is retried after `MinInterval`. Only signals whose
JSON changed since the last batch are sent. While the page is hidden nothing is
sent and the engine keeps running; the next batch carries the newest state.
Measured in headless Chrome 153: an engine setting the view every 10 ms
rendered into an island about 10 times per second.

Keep signals small and separate. Every reader of a signal re-renders when any
field of it changes, so put the fast fields in their own signal.

### Run the same engine natively in a desktop app

A GoSX desktop app can host the engine in its own process instead. The island,
its signals and its calls stay the same; only the transport changes. The Go
engine code is written once against `engine/host`, which holds the
transport-neutral half of `wasm.NewView` and `wasm.ServeCalls`:

| Need | `engine/host` | In a worker (`engine/wasm`) |
|---|---|---|
| Publish state at the page's pace | `host.NewView(ctx, opts)`; `*host.View` is a `host.Publisher` (`Set(map[string]any) error`) | `wasm.NewView` (an alias of the same type) |
| Serve calls | `host.ServeCalls(ctx, channel, opts, handle)` | `wasm.ServeCalls` (same rules) |

`wasm.View`, `wasm.ViewOptions` and `wasm.CallOptions` are aliases of the `host`
types, so the pacing, acknowledgement, timeout and crash-error rules are
written once. Write the engine against `host.Publisher` and plain call
handlers, and put each transport in a small adapter.

The desktop adapter is `desktop/enginehost`:

```go
app, _ := desktop.New(desktop.Options{NativeBridge: true /* ... */})
engines := enginehost.New(app.Bridge(), enginehost.Options{})
defer engines.Close()
engines.Register("Sim", func(in *enginehost.Instance) (host.Handle, error) {
    view := host.NewView(in, host.ViewOptions{})
    calls, err := host.ServeCalls(in, "sim.cmd", host.CallOptions{}, handle)
    ...
})
```

The page declares the engine as before (`Thread: engine.ThreadWorker`, the
component name `"Sim"`). When the page runs in a desktop window, the runtime
sees `window.gosxDesktop` and asks the host to attach the engine for that
component. If the host has registered it, no Worker starts and no wasm loads;
the status signal reads `running` with `Thread: "native"`. If the host has no
engine for the component, or the page runs in a browser, the Worker starts as
described above. `ForceWorkerFallback` and `window.__gosx_engine_native_disable
= true` skip the native engine. Note that an app that ships only a desktop
engine still needs a `WASMPath`: the config requires one, and a browser would
fetch it.

What stays the same for the island:

- Views arrive as signal batches no faster than `MinInterval`, and the next one
  waits for the page to apply the previous one (`AckTimeout` frees a lost
  acknowledgement). Batches the page rejects are sent again. A hidden window
  receives nothing until it shows again.
- A call result has the same shape. The page watchdog still fails a call with
  `timeout` when the host does not answer in `CallOptions.Timeout`. If the
  engine calls `Instance.Fail(err)`, the status becomes `crashed` and pending and
  later calls fail with `worker-crashed`. A handler that returns an error or
  panics gives `Error` as before.
- A call written before the engine attached is handled from the signal's
  current value unless the result already answered it.

What differs: the engine runs next to the window, so a call handler may block the
engine goroutine but never the window thread (each instance runs its messages one
at a time on its own goroutine). One instance exists per attach: a page reload
attaches again, and the host disposes the old instance before it mounts the new
one. Input forwarding, application messages and netplay are not carried over the
native channel. The wire protocol is the worker message protocol below, carried
as `gosx.engine.attach` and `gosx.engine.post` requests and `gosx.engine.msg`
events on the desktop bridge. The bridge caps page messages at 64 KiB and 200 per
second by default (`Options.BridgeLimit`); a view batch travels host to page and
is not capped. The native channel works wherever the typed bridge does; see
`desktop` for which platforms have it. `examples/desktop-engine` is a complete
small app.

### Two engines on one page

Shared signal names are global to the page. Two engines that use `$bv.cmd` would
both answer it. Prefix every signal an engine owns with a name unique to it
(`bv.cmd`, `bv.view`, `bv.hud`; for a second engine `lab.cmd`, `lab.view`). The
status signal already carries the engine ID. GoSX does not enforce this.

### Vessel pose and cutaway from a worker

`scene.DispatchVesselPose` and `scene.DispatchVesselCutaway` work unchanged in a
worker. The worker forwards the pose as a transferred frame and the cutaway as a
message to the page's vessel controller. The call cannot wait for the page, so
the worker returns what the page last answered for that target (and cutaway
node). Until the page answers once, the call returns the error that the main
thread gives for a mount with no vessel yet. After the first accepted frame it
returns no error, and if the mount stops showing a vessel the error returns one
round trip later. The page counts rejected calls in
`__gosx.engineWorker.get(id).stats().vesselRejected`.

### Not covered

An island cannot receive bytes (checkpoints, traces). Use `Port.PostBytes` and
page JavaScript for those. A call or write to a signal can come from any island
or page script, so treat call arguments as untrusted input.

## A fixed-rate loop in the worker

`requestAnimationFrame` follows the page's display. In a worker, `game/loop` and
`game/host.FrameSourceJS` use the worker's own timer clock instead (60 Hz by
default, or the engine prop `workerFrameHz`). Frames land on a fixed grid of
1000/hz ms slots; a late timer skips to the next slot and never bursts to
catch up.

```go
rt := game.New(game.Config{FixedStep: time.Second / 60, MaxSubsteps: 8, MaxDelta: time.Second, Systems: systems})
driver := loop.New(rt, loop.NewFrameSourceJS())
driver.RunWhileHidden = true // see below
driver.Start(render)
```

`Hidden()` is correct in a worker: the page forwards `document.hidden`. By
default a `loop.Driver` pauses while hidden, as on the main thread. Set
`RunWhileHidden` for a simulation that must stay in step with other players, such
as a rollback match, so a hidden tab does not stall the opponent.

`Port` also has `SetFrameRate(hz)`, `FrameStats()` (measured cadence), `Hidden()`,
`OnVisibility(fn)` and `Now()`.

### Measured: busy page and hidden tab

Chrome 153 on Linux, one 60 Hz `game.Runtime` per engine. Numbers come from
`make test-engine-worker-browser` and `make test-engine-worker-hidden`.

| Case | Worker engine | Main-thread engine |
|---|---|---|
| Page main thread busy 120 ms of every 200 ms (frames per second, longest gap) | 60.0 fps, 17.7 ms | 34.3 fps, 130 ms |
| Tab hidden for 25 s, clock ticks per second | 60.0 | 0 (`requestAnimationFrame` stops) |
| Tab hidden for 25 s, a 16 ms `setInterval` on the page | not applicable | 1.0 to 1.6 per second |
| Tab hidden, fixed steps per second with `RunWhileHidden` | 60 | not applicable |
| Tab hidden, fixed steps per second without it | 0 (`Hidden()` is true) | 0 |

The longest tick gap in the worker during the hidden runs was 27 ms (two runs, 25 s each). I did not
test Chrome's stricter throttling after a tab has been hidden for five minutes.
The hidden-tab test needs a headed Chrome and a display: set
`ENGINE_WORKER_HEADFUL_CHROME` and `DISPLAY`. It uses a second tab to hide the
first, because Playwright keeps pages "visible".

## Drive Scene3D from the worker

The worker sends binary frames to the page as transferred `ArrayBuffer`s, and
the page hands them to the same functions a main-thread engine calls:

| Go call | Page function |
|---|---|
| `scene.DispatchPoseFrame` (GSP2) | `__gosx.scene3d.dispatchPoseFrame` |
| `scene.DispatchMotionFrame` (GSP3) | `__gosx.scene3d.dispatchMotionFrame` |
| `scene.DispatchClipFrame` (GSP4) | `__gosx.scene3d.dispatchClipFrame` |
| `scene.StreamWriter.Apply` (GSXI) | `mount.__gosxScene3DHandle.applyInstanceStream` |

These Go calls work unchanged. The worker supplies the `__gosx.scene3d`
functions they look up, and each frame's buffer moves to the page without a
copy. A frame the renderer rejects shows up as a `gosx:engine:worker-frame-error`
event, because the Go call returns before the page applies the frame. For a
`StreamWriter`, `applied` means "queued for the page".

`scene.Surface` also works: `DispatchCommands`, `OnFrameSubmitted`,
`CancelFrameRequest` and `Update` send events and calls to the page. The page
accepts only `gosx:scene3d:` events and the `updateSceneProps` call. `Resize` and
`LoseContext` return an error in a worker, because the page owns the canvas.

`ctx.Scene3DByID("arena")` targets a mount by DOM id (a worker has no document to
look it up in). Reading the camera (`Scene3D.Camera`) is main-thread only;
`SetCamera` works.

Shared signals: `ctx.SetSignals` and `ctx.SetChangedSignals` send one JSON batch as
a transferred buffer, and the page applies it exactly as on the main thread.
`ctx.SubscribeSignal` works too, and a subscription primes the worker with the
current value. A worker can read a signal with `ctx.SignalJSON` only after it
subscribes to it.

In a worker, Go code cannot use `window` or `document`: `ctx.Mount()` is null.

Proof it works: a worker engine streams GSXI frames for one instanced box. The
box's pixels in the page canvas move from x = 128 px to x = 299.5 px, and the
same engine on the main thread ends at the same 299.5 px.

## Input

The page forwards input to an engine that declares the matching capability
(`CapKeyboard`, `CapPointer`, `CapGamepad`). The page samples because DOM events
and `navigator.getGamepads` exist only there; the engine in the worker decides
what to do with them.

```go
input, ok := host.NewForwardedInput() // game/host; ok is false outside a worker
input.Subscribe(func(e host.InputEvent) {
    // e.Kind "key" | "pointer" | "pad" | "blur"
    // e.AtMS: when it happened, on the worker's clock. e.Delay(): time to reach the worker.
})
input.KeyDown("KeyA")
poller.Poll(input, rt.Input()) // input is a game/gamepad Source too
```

Rules: key auto-repeat and typing in text fields are not forwarded; a gamepad is
polled every 8 ms while the page is visible and sent only when a button or
stick changes by more than 0.001; `blur` (the page lost focus or was hidden)
releases every held key, button and pad. In the headless test the longest delay
from a page event to the worker was 14 ms, which includes the pad polling step.
The page's own `$input.*` signals still exist.

## Netplay

`netplay/client.Dial` works in a worker with no change. `RTCPeerConnection` does
not exist in a worker (checked in Chrome 153), so the page owns it. Datagrams
take one of two routes, and the worker chooses the faster one the browser
allows:

- **Transfer.** The page creates the unordered data channel and transfers it to
  the worker in the same task (Chrome allows this only right after creation).
  The worker then sends and receives datagrams itself. The page's main thread
  is not in the path.
- **Relay.** If the browser refuses the transfer, the page forwards each
  datagram over the port as a transferred buffer.

I chose transfer first because the point of a worker is to keep the match away
from main-thread work. Measured with the page's main thread busy 120 ms of every
200 ms (300 frames against a native peer through the Go relay, no desyncs in any
case):

| Where the peer runs | Round trip, mean (max) | Rollbacks |
|---|---|---|
| Worker, channel transferred | 16.7 ms (17.0 to 17.3) | 0 |
| Worker, page relays datagrams | 40.6 to 46.5 ms (56 to 66) | 24 to 25 |
| Main thread | 42.2 to 42.3 ms (50 to 61) | 0 |

Each row gives the range of two runs.

Signaling still goes through Go over the hub, and the WebSocket fallback runs
inside the worker. Set `window.__gosx_engine_worker_net_transfer = false` to
force the relay route. The page must still call `RequireFeature("netplay")` so
the page-side binding loads. I could not run the transfer route in Firefox or
Safari; they use the relay route when their `RTCDataChannel` is not
transferable.

## Message protocol

Version 1. Every message is an object with a type field `t`. Binary payloads
travel as `ArrayBuffer`s in the transfer list. The page and worker check the
version at boot (`init` and `booted` carry `v`).

Page to worker:

| `t` | Fields | Meaning |
|---|---|---|
| `init` | `v, id, component, programRef, wasmExec, props, capabilities, hidden, clock, frameHz, rtc` | Boot: load wasm_exec.js and the module, run it, register the factory. URLs are absolute. |
| `mount` | | Call the Go factory. |
| `dispose` | | Run the Go handle's `Dispose`; the worker answers `disposed`. |
| `reply` | `rid, ok, value, error` | Answer to a worker request. |
| `visibility` | `hidden, at` | Page visibility changed. |
| `signal` | `sid, name, json, initial` | A subscribed shared signal changed. |
| `app` | `name, json, buf` | Application message to the engine. |
| `input` | `ev` | A forwarded input event (`ts` is epoch milliseconds). |
| `mount.event` | `lid, json` | A listened Scene3D mount event. |
| `net.signal`, `net.state`, `net.mode`, `net.channel`, `net.data` | | Netplay: signaling out, connection state, route, the transferred channel, relayed datagrams. |
| `fault` | `where, message` | The page rejected something the worker sent. |
| `ack` | `seq, ok` | The page handled the view batch numbered `seq`; `ok` is false if it rejected the batch, and the view sends it again. |
| `vessel.result` | `kind, target, nodeID, ok` | The page's answer for a vessel pose or cutaway target, sent on the first call and when it changes. |

Worker to page:

| `t` | Fields | Meaning |
|---|---|---|
| `booted`, `mounted`, `disposed` | | Lifecycle acknowledgements. |
| `fatal` | `code, message` | Boot or mount failure, or a crash. |
| `log` | `level, text` | Console output. |
| `signals` | `changedOnly, buf, seq?` | A signal batch as UTF-8 JSON bytes. With `seq` (a view batch) the page answers `ack`. |
| `calls` | `channel, timeout, recent, zero` | The engine serves calls on `channel`; the page starts its watchdog. |
| `vessel.cutaway` | `target, nodeID, json` | `scene.DispatchVesselCutaway`. Vessel poses travel as `frame` with `kind: "vessel-pose"`. |
| `signal.set`, `subscribe`, `unsubscribe` | | Single write and subscription control. |
| `scene3d`, `navigate` | `rid, ...` | Requests that answer with `reply`. |
| `emit` | `name, detail` | `ctx.Emit`. |
| `frame` | `kind, target, buf, options` | A GSP2, GSP3, GSP4 or GSXI frame. |
| `app` | `name, json, buf` | Application message to the page. |
| `mount.op`, `mount.listen`, `mount.unlisten` | | Scene3D mount events and one renderer call. |
| `net.open`, `net.signal.in`, `net.send`, `net.worker-state`, `net.close` | | Netplay requests. |

Application messages: in Go, `port.Post(name, v)`, `port.PostBytes(name, b)` and
`port.OnMessage(fn)`. On the page, `__gosx.engineWorker.get(id)` has `post`,
`onMessage`, `stats()`, `netModes()` and `terminate()`; every message also fires a
`gosx:engine:message` event on `document`.

## Tests

| Command | What it covers |
|---|---|
| `node --test client/runtime/host/engine-worker.test.js` | The page and worker halves against a fake `Worker` and a fake netplay binding. |
| `make test-engine-worker-wasm` | The Go port, frame source, `Surface`, `StreamWriter` and input on js/wasm. |
| `make test-engine-worker-browser` | A real worker in headless Chromium: boot, fallback, 60 Hz under a busy page, Scene3D driven by frames, input, and a real island that calls the worker, sees its paced view, gets a timeout and a crash error, and runs the same code under a forced fallback. |
| `make test-engine-worker-hidden` | Worker clocks in a really hidden tab (headed Chrome). |
| `make test-netplay-browser` | Real matches against a native peer, including the four worker routes. |

Set `ENGINE_WORKER_CHROME` (or `NETPLAY_CHROME`) to a Chromium or
chrome-headless-shell binary when Playwright's own download does not run.
