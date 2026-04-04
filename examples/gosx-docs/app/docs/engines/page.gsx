package docs

func Page() Node {
	return <div>
		<section id="engine-model">
			<h2 class="chrome-text">Engine Model</h2>
			<p>
				An engine is a dedicated compute surface — an isolated WASM or JS runtime that
				owns a specific browser capability. Where islands handle reactive text and
				attribute updates in the main document, engines handle heavy compute: 2D canvas
				drawing, 3D rendering, audio processing, and background workers.
			</p>
			<p>
				Each engine has a mount point in the page — a
				<span class="inline-code">&lt;canvas&gt;</span>,
				an offscreen worker, or a typed port — and a compiled Go program that runs
				inside that context. The main page runtime and the engine communicate over a
				structured message channel; the engine never touches the main DOM directly.
			</p>
			{CodeBlock("go", `// Declare an engine in the route loader.
func Load(ctx *route.RouteContext, page route.FilePage) (any, error) {
    eng := engine.New(engine.Options{
        Capabilities: []engine.Cap{engine.CapCanvas, engine.CapPointer},
        Tier:         engine.TierOptimized,
        Program:      MyCanvasProgram,
    })
    return map[string]any{"engine": eng}, nil
}`)}
			<p>
				Engines are declared server-side and serialised into the page as a
				<span class="inline-code">data-engine</span>
				descriptor. The bootstrap runtime reads the descriptor, provisions the requested
				capability, and starts the engine program before the first animation frame.
			</p>
		</section>

		<section id="capability-tiers">
			<h2 class="chrome-text">Capability Tiers</h2>
			<p>
				Every engine declares which browser capabilities it needs. GoSX checks capability
				availability at mount time and selects a fallback tier if the primary capability
				is unavailable — so a page authored for WebGPU degrades gracefully to WebGL on
				older hardware.
			</p>
			<div class="engines-cap-grid">
				<div class="engines-cap-group glass-panel">
					<h3 class="engines-cap-group__title">Render</h3>
					<div class="engines-cap-list">
						{CapabilityTag("canvas")}
						{CapabilityTag("webgl")}
						{CapabilityTag("webgl2")}
						{CapabilityTag("webgpu")}
					</div>
				</div>
				<div class="engines-cap-group glass-panel">
					<h3 class="engines-cap-group__title">Input</h3>
					<div class="engines-cap-list">
						{CapabilityTag("pointer")}
						{CapabilityTag("keyboard")}
						{CapabilityTag("gamepad")}
					</div>
				</div>
				<div class="engines-cap-group glass-panel">
					<h3 class="engines-cap-group__title">I/O</h3>
					<div class="engines-cap-list">
						{CapabilityTag("audio")}
						{CapabilityTag("storage")}
						{CapabilityTag("fetch")}
					</div>
				</div>
			</div>
			<p>
				The tier system controls how aggressively the runtime optimises:
			</p>
			<ul>
				<li>
					<span class="inline-code">engine.TierOptimized</span>
					— requests the highest capability available. Selects WebGPU if present,
					falls back to WebGL2, then WebGL, then canvas 2D.
				</li>
				<li>
					<span class="inline-code">engine.TierBalanced</span>
					— prefers WebGL2 or WebGL. Skips WebGPU even if available. Good for
					content that must run consistently across mid-range hardware.
				</li>
				<li>
					<span class="inline-code">engine.TierConservative</span>
					— canvas 2D only. Guaranteed to work on any browser with a display.
				</li>
			</ul>
			{CodeBlock("go", `engine.New(engine.Options{
    Capabilities: []engine.Cap{
        engine.CapWebGPU,
        engine.CapWebGL2,
        engine.CapCanvas,
    },
    Tier: engine.TierOptimized,
})`)}
		</section>

		<section id="canvas-surface">
			<h2 class="chrome-text">Canvas Surface</h2>
			<p>
				The canvas surface is the simplest engine type. Mount it with
				<span class="inline-code">engine.CapCanvas</span>
				and the bootstrap runtime will provide an
				<span class="inline-code">OffscreenCanvas</span>
				(or a proxied 2D context on browsers without offscreen support) to the engine
				program's entry point.
			</p>
			{CodeBlock("gosx", `<div class="canvas-mount">
    <canvas
        data-engine={data.engine}
        width="800"
        height="450"
        aria-label="Interactive canvas surface"
    ></canvas>
</div>`)}
			{CodeBlock("go", `// Engine program — runs inside the canvas context.
func MyCanvasProgram(ctx engine.Context) {
    c := ctx.Canvas2D()

    ctx.OnFrame(func(dt float64) {
        c.ClearRect(0, 0, ctx.Width(), ctx.Height())
        c.SetFillStyle("#D4AF37")
        c.FillRect(10, 10, 100*dt, 60)
    })

    ctx.OnPointer(func(ev engine.PointerEvent) {
        // React to mouse/touch without touching the main DOM.
    })
}`)}
			<p>
				The engine program has no access to the main document. All communication with
				the page goes through typed message ports — the engine receives commands and
				sends events back via
				<span class="inline-code">ctx.Send</span>
				and
				<span class="inline-code">ctx.OnMessage</span>.
			</p>
		</section>

		<section id="webgl-webgpu">
			<h2 class="chrome-text">WebGL / WebGPU</h2>
			<p>
				For 3D and shader-heavy workloads, declare the appropriate render capability and
				the runtime provisions a GPU context for the engine. The GoSX 3D engine uses this
				path internally — the same capability system powers both user-defined engines and
				the built-in
				<span class="inline-code">scene3d</span>
				primitives.
			</p>
			{CodeBlock("go", `// WebGPU engine with fallback to WebGL2.
eng := engine.New(engine.Options{
    Capabilities: []engine.Cap{engine.CapWebGPU, engine.CapWebGL2},
    Tier:         engine.TierOptimized,
    Program:      func(ctx engine.Context) {
        gpu := ctx.WebGPU() // nil if WebGPU unavailable; use ctx.WebGL2() instead
        if gpu == nil {
            gl := ctx.WebGL2()
            runWebGLPath(ctx, gl)
            return
        }
        runWebGPUPath(ctx, gpu)
    },
})`)}
			<p>
				Capability negotiation happens at runtime, not at author time. The engine program
				receives whichever context the browser can provide. Writing a two-path program
				covers the full capability range without separate builds.
			</p>
			<section class="callout">
				<strong>Ownership transfer</strong>
				<p>
					When a canvas element is handed to a WebGL or WebGPU engine, the runtime
					transfers ownership to the engine's offscreen context. Subsequent attempts to
					call
					<span class="inline-code">getContext</span>
					on the element from the main thread will fail. This is a browser constraint,
					not a GoSX limitation.
				</p>
			</section>
		</section>

		<section id="workers">
			<h2 class="chrome-text">Workers</h2>
			<p>
				Not every engine needs a render surface. Worker engines run Go programs in a
				background thread with no canvas — useful for CPU-intensive tasks that should
				not block the main thread: image processing, data transformation, physics
				simulation, and cryptographic operations.
			</p>
			{CodeBlock("go", `// Worker engine — no canvas capability.
eng := engine.New(engine.Options{
    Capabilities: []engine.Cap{engine.CapFetch, engine.CapStorage},
    Tier:         engine.TierBalanced,
    Program:      func(ctx engine.Context) {
        ctx.OnMessage(func(msg engine.Message) {
            switch msg.Type {
            case "process":
                result := heavyCompute(msg.Payload)
                ctx.Send(engine.Message{Type: "result", Payload: result})
            }
        })
    },
})`)}
			<p>
				Worker engines communicate with the page through the same typed message port
				as render engines. From the page side, write to the engine's input port and
				subscribe to its output via island signal bindings or lifecycle script handlers.
			</p>
		</section>

		<section id="engine-programs">
			<h2 class="chrome-text">Engine Programs</h2>
			<p>
				An engine program is a Go function with the signature
				<span class="inline-code">func(engine.Context)</span>.
				It runs in the engine's isolated context. The context provides the capability
				handle, the message ports, and the frame / event hooks.
			</p>
			{CodeBlock("go", `// engine.Context interface (abbreviated).
type Context interface {
    // Capability handles.
    Canvas2D() Canvas2DContext
    WebGL2()   WebGL2Context
    WebGPU()   WebGPUContext

    // Dimensions (updated on resize).
    Width()  float64
    Height() float64

    // Frame loop.
    OnFrame(fn func(dt float64))
    CancelFrame()

    // Input events.
    OnPointer(fn func(PointerEvent))
    OnKey(fn func(KeyEvent))

    // Messaging.
    Send(msg Message)
    OnMessage(fn func(Message))

    // Lifecycle.
    OnDispose(fn func())
}`)}
			<p>
				Engine programs are registered at package init time and referenced by name in
				the engine descriptor. This allows the compiler to dead-strip unreferenced
				programs from the WASM binary at build time, keeping the shipped binary small.
			</p>
			{CodeBlock("go", `func init() {
    engine.Register("my-canvas", MyCanvasProgram)
    engine.Register("bg-worker", BackgroundWorkerProgram)
}

// In the route loader, reference by name.
eng := engine.NewByName("my-canvas", engine.Options{
    Capabilities: []engine.Cap{engine.CapCanvas, engine.CapPointer},
    Tier:         engine.TierOptimized,
})`)}
		</section>
	</div>
}
