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
image. Optional `capabilities` is checked before uploads; optional `msg_fault`
reports a fault without draining the ring (see the fault rules below); optional
shared-bank exports amortize expensive synthesis-bank initialization across
replacements.

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
backend fault, usually without waiting for `b` (see below). `q` reports timing
histograms, memory and underruns; `p` resets the histogram. Closing the typed
node sends `close`, releases the reactor, disconnects the node and closes ports.

The processor drains the active reactor's message ring every eighth render
quantum. It loses no message while the page holds the `m` buffer: it parks the
first batch drained in that time, sends it as soon as `b` returns the buffer, and
stops draining until then. Later messages wait in the reactor's ring, in order,
and reach the page in later `m` batches. An image replacement retires the old
reactor without delivering the messages still queued in it; the processor drains
that reactor for faults only, and only while the 5 ms crossfade lasts.

A fault is a record in the ring, so a processor that has stopped draining cannot
see one unless the reactor tells it. When `f` arrives depends on where the fault is:

- A fault in the parked batch is reported at once with `f` and playback stops.
  Its record also arrives in order in `m`, so a page that handles both should
  ignore the second.
- A reactor that exports `msg_fault` is polled at each drain point while the
  processor is not draining. `msg_fault()` returns 0 while the reactor is healthy
  and `0x10000` plus the fault's 16-bit argument once it has faulted; it must not
  touch the ring. The processor then posts `f` and stops playback at once, however
  long the page holds the buffer. It drains only while playing, so messages still
  in the ring are not delivered.
- Without `msg_fault`, a fault queued after the parked batch waits for the first
  drain after `b`. Playback goes on until then, and a page that never returns `b`
  never learns of the fault.

Size the reactor's own ring for the longest page stall it must survive;
`MessageCapacity` sizes one batch, not the ring. A reactor that faults when its
ring fills now stops playback when a stall outlasts the ring, where the music
used to go on and the messages were lost. The processor cannot see a reactor that
drops messages without a fault.

Validation runs in Go and the processor. Native asset tests enforce 24 KiB raw
and 6 KiB gzip limits. The Node suite exercises a separate mono backend with
different record sizes and export names. Applications should additionally test
their actual kernel and listen/check PCM in a browser.
