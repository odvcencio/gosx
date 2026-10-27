# WASM audio worklet

GoSX owns the external `gosx-wasm-audio` processor, its audio-thread render loop,
bounded command/message buffers, memory views, diagnostics and shutdown. A
backend owns its WASM synthesis reactor, project format and command meanings.
There is no dependency on Cicada or any other synthesizer in this package.

Mount the server-only `worklet.Handler()` under a same-origin asset prefix. Load that prefix plus
`worklet.Path()` with `HostJS.AddWorkletModule`, compile the backend's WASM once
with `HostJS.CompileWASM`, then create independent nodes with
`HostJS.NewWASMWorklet`. Pass a validated `Backend`, opaque image bytes and
backend-extracted `ImageInfo`. The processor URL contains its source digest, so
cached code cannot mix with a newer host. It works with an Activity proxy prefix
and an external-script CSP; no source is generated in the application.

Browser imports retain the typed ABI and portable `Path`/`Source` APIs, but
exclude the HTTP handler and its server dependencies. Processor assets are
hashed only when requested, so unused audio worklets add no asset initialization
to applications that use only the ordinary typed audio host.

The reactor ABI uses `gosx_audio_` exports by default; `Backend.ExportPrefix`
supports other export names. It requires an exported `memory`, `_initialize`,
`init(rate, maxFrames, channels)`, `render(frames)`, `out_ptr`, `cmd_ptr`,
`cmd_cap`, `cmd_commit(count)`, `msg_ptr`, and `msg_drain`. Output is planar
float32 with `MaxFrames` samples per channel. Record sizes and message capacity
come from the backend descriptor. `project_alloc` is required when supplying an
image. Optional `capabilities` is checked before uploads; optional shared-bank
exports amortize expensive synthesis-bank initialization across replacements.

The processor splits browser render quanta into `MaxFrames` blocks and reuses
its typed views. If reactor memory grows, it refreshes detached views. Kernels
should allocate during initialization and avoid growth in steady rendering.
Render exceptions silence the entire block and stop the processor. Record
validation rejects an entire command batch before committing any prefix.

Use `Transport` only for engines with musical seek/play/stop semantics. It
describes opcode numbers and tick offsets, enables boundary-aligned image
replacement, queues launch commands and crossfades the old/new instances for
5 ms. Without it, rendering starts after initialization and commands stay
opaque. Image replacement metadata must be validated by the backend again.

Messages retain a small transferable protocol: `c` submits a `Uint8Array` of
commands; `i` supplies a new image and `info`; `r` acknowledges initialization;
`m` transfers engine messages (`n` valid bytes); `b` returns that buffer. `e`
reports a fatal host error, `x` rejects a request, and `f` reports a configured
backend fault even while the message buffer is outstanding. `q` reports timing
histograms, memory and underruns; `p` resets the histogram. Closing the typed
node sends `close`, releases the reactor, disconnects the node and closes ports.

The processor drains the active reactor's message ring every eighth render
quantum and drops nothing while the page holds the `m` buffer. It parks one batch
and sends it as soon as `b` returns the buffer. Past that it stops draining, so
later messages wait in the reactor's ring, in order, until the page catches up.
A fault found while the buffer is out is reported at once with `f`, and its
record still arrives in order in `m`. A fault raised after draining has stopped
is reported at the first drain after `b`. Because the ring then holds what a
page stall produces, size `MessageCapacity` for that and have the reactor
report a full ring with its fault record; the processor cannot see a reactor
that drops messages silently.

Validation runs in Go and the processor. Native asset tests enforce 24 KiB raw
and 6 KiB gzip limits. The Node suite exercises a separate mono backend with
different record sizes and export names. Applications should additionally test
their actual kernel and listen/check PCM in a browser.
