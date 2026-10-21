# Engine workers: run a Go WASM engine off the main thread

A Go WASM engine normally runs on the page's main thread, so a slow frame or a
hidden tab slows the simulation down with it. Set `Thread: engine.ThreadWorker`
and GoSX starts a dedicated Web Worker, loads the engine's Go module inside it,
and keeps the page for what only the page can do: the DOM, Scene3D, shared
signals, input and the `RTCPeerConnection`.

You write Go only. No JavaScript.

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

The engine falls back to the main thread and says why. Nothing else in your Go
code changes.

| Reason code | When |
|---|---|
| `worker-unsupported` | The browser has no `Worker`. |
| `worker-disabled` | The page set `window.__gosx_engine_worker_disable = true`. |
| `worker-toolchain-unsupported` | The engine is built with TinyGo. |
| `worker-create-failed` | `new Worker(...)` threw (for example a CSP block). |
| `worker-script-error` | The worker script failed to load or run. |
| `worker-boot-timeout` | The module did not register its factory in time (10 s, or `window.__gosx_go_wasm_factory_timeout_ms`). |
| `worker-fetch-status`, `worker-wasm-exec-missing`, `worker-protocol`, `worker-boot-failed` | The worker reported a boot error or a protocol mismatch. |

A fallback appears as a runtime issue (`__gosx.listIssues()`, type `worker`,
`fallback: "main-thread"`), a telemetry warning, and a
`gosx:engine:thread-fallback` event on `document`. In Go, `ctx.Thread()` is
then `engine.ThreadMain` and `ctx.ThreadFallbackReason()` returns the code. A
worker that crashes after it mounted is not restarted: the page reports
`worker-crashed`, fires `gosx:engine:worker-error`, and ends the engine.

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

Worker to page:

| `t` | Fields | Meaning |
|---|---|---|
| `booted`, `mounted`, `disposed` | | Lifecycle acknowledgements. |
| `fatal` | `code, message` | Boot or mount failure, or a crash. |
| `log` | `level, text` | Console output. |
| `signals` | `changedOnly, buf` | A signal batch as UTF-8 JSON bytes. |
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
| `make test-engine-worker-browser` | A real worker in headless Chromium: boot, fallback, 60 Hz under a busy page, Scene3D driven by frames, input. |
| `make test-engine-worker-hidden` | Worker clocks in a really hidden tab (headed Chrome). |
| `make test-netplay-browser` | Real matches against a native peer, including the four worker routes. |

Set `ENGINE_WORKER_CHROME` (or `NETPLAY_CHROME`) to a Chromium or
chrome-headless-shell binary when Playwright's own download does not run.
