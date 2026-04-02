package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Engines</span>
			<p class="lede">
				Engine kind picks the mount model. Capability declarations spell out which browser powers the runtime is allowed to depend on.
			</p>
		</div>
		<h1>
			GoSX engines are explicit about browser power instead of hiding it behind hydration.
		</h1>
		<p>
			An island is still constrained DOM work in the shared VM. An engine is for the cases that do not fit that model: canvas rendering, WebGL scenes, background workers, managed video playback, low-level input, or audio-driven loops. The key distinction is that
			<span class="inline-code">Kind</span>
			and
			<span class="inline-code">Capabilities</span>
			mean different things. Kind chooses the execution shape. Capabilities declare which browser APIs the engine expects.
		</p>
		<section class="feature-grid">
			<div class="card">
				<strong>Surface</strong>
				<p>
					A mount-owning engine for canvas, WebGL, WebGPU, pixel buffers, or any managed browser surface.
				</p>
			</div>
			<div class="card">
				<strong>Worker</strong>
				<p>
					Background compute with no DOM mount. Use it for parsing, search, simulation, or long-running client work.
				</p>
			</div>
			<div class="card">
				<strong>Video</strong>
				<p>
					A framework-owned managed player mount. GoSX handles the
					<span class="inline-code">&lt;video&gt;</span>
					lifecycle, source application, HLS loading, sync signals, and subtitle plumbing.
				</p>
			</div>
			<div class="card">
				<strong>Built-ins still declare shape</strong>
				<p>
					Helpers like
					<span class="inline-code">Scene3D</span>
					are still engines underneath. They just ship with sensible defaults like canvas, WebGL, and animation already declared.
				</p>
			</div>
		</section>
		{DocsCodeBlock("go", `ctx.Engine(engine.Config{
	    Name: "sampler",
	    Kind: engine.KindSurface,
	    Capabilities: []engine.Capability{
	        engine.CapCanvas,
	        engine.CapAnimation,
	        engine.CapPointer,
	        engine.CapKeyboard,
	    },
	    WASMPath: "/engines/sampler.wasm",
	}, gosx.El("div", gosx.Text("Preparing engine...")))`)}
		<h2>The supported capability surface</h2>
		<p>
			These are the currently supported capability tokens in
			<span class="inline-code">engine.Config.Capabilities</span>
			. They are declarations, not magic. They tell the runtime what the engine intends to use, and they make the page contract inspectable instead of implicit.
		</p>
		<section class="feature-grid">
			<div class="card">
				<strong>video</strong>
				<p>
					Managed media playback. Most useful with
					<span class="inline-code">engine.KindVideo</span>
					when the framework owns the player shell.
				</p>
			</div>
			<div class="card">
				<strong>canvas</strong>
				<p>
					2D or immediate-mode drawing surfaces, including custom raster rendering and classic game loops.
				</p>
			</div>
			<div class="card">
				<strong>webgl</strong>
				<p>
					GPU-backed 3D scenes and custom shaders. The built-in
					<span class="inline-code">Scene3D</span>
					path declares this automatically.
				</p>
			</div>
			<div class="card">
				<strong>webgpu</strong>
				<p>
					Next-generation GPU work where the engine is explicitly targeting WebGPU instead of WebGL.
				</p>
			</div>
			<div class="card">
				<strong>pixel-surface</strong>
				<p>
					A managed RGBA framebuffer driven by the runtime. Pair it with
					<span class="inline-code">canvas</span>
					when you want low-level pixels with framework-owned scaling.
				</p>
			</div>
			<div class="card">
				<strong>animation</strong>
				<p>
					Frame scheduling for loops, interpolation, or simulation ticks that need a real browser animation clock.
				</p>
			</div>
			<div class="card">
				<strong>storage</strong>
				<p>
					Client persistence such as local storage or cached user preferences that belong to the engine runtime.
				</p>
			</div>
			<div class="card">
				<strong>fetch</strong>
				<p>
					Network access from the engine for source loading, background queries, or media fetches.
				</p>
			</div>
			<div class="card">
				<strong>audio</strong>
				<p>
					Sound playback, media routing, or audio-driven timing. Video engines naturally pair with this capability.
				</p>
			</div>
			<div class="card">
				<strong>worker</strong>
				<p>
					Worker-thread style compute. Use it when the engine is explicitly structured around background execution instead of a DOM mount.
				</p>
			</div>
			<div class="card">
				<strong>gamepad</strong>
				<p>
					Game controller input for games, simulations, or tool surfaces that expect pad semantics.
				</p>
			</div>
			<div class="card">
				<strong>keyboard</strong>
				<p>
					Keyboard-driven controls and shortcuts owned by the engine rather than by island DOM handlers.
				</p>
			</div>
			<div class="card">
				<strong>pointer</strong>
				<p>
					Pointer movement, drag, hover, and hit-testing for mounted surfaces that need direct spatial input.
				</p>
			</div>
		</section>
		<section class="callout">
			<strong>Rule of thumb</strong>
			<p>
				Use the smallest honest declaration. Pick the engine
				<span class="inline-code">Kind</span>
				that matches the mount model, then list only the capabilities the browser code actually depends on. That keeps the runtime contract readable and keeps docs, manifests, and implementation in sync.
			</p>
		</section>
		<div class="hero-actions">
			<Link class="cta-link" href="/docs/runtime">Back to runtime</Link>
			<Link class="cta-link primary" href="/docs/video">Continue to video</Link>
		</div>
	</article>
}
