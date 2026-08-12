package docs

func Page() Node {
	return <div>
		<section id="quick-start" class="docs-section-block">
			<h2>
				My Geometry Is Invisible — What Do I Run
			</h2>
			<p>
				GoSX ships four tiers of Scene3D debug tooling already. Most invisible-geometry and untrustworthy-capture bugs need only the first two commands below. Read the rest of this page when those do not explain the symptom.
			</p>
			<ol>
				<li>
					Render the scene with the central processing unit (CPU) reference renderer and open the PNG:
					<span class="inline-code">
						gosx scene render scene.json --out scene.png
					</span>
					. This path skips the browser's frustum cull entirely, so if an object is present here but missing in the browser, the bug is in the browser-side cull or camera math, not in the scene graph. See
					<a href="#cpu-reference-renderer" class="inline-link">CPU Reference Renderer</a>
					.
				</li>
				<li>
					When a scene looks wrong on real hardware, read the render-truth attributes first:
					<span class="inline-code">data-gosx-scene3d-render-post-chain</span>
					names every authored post effect and whether it actually dispatched, and
					<span class="inline-code">data-gosx-scene3d-render-mesh-drawn</span>
					counts meshes that reached the framebuffer. See
					<a href="#render-truth-attributes" class="inline-link">Render-Truth Attributes</a>
					. To get a dump from a machine with a real GPU, run
					<span class="inline-code">node scripts/windows-scene3d-probe.mjs</span>
					on that machine and share the folder it writes.
				</li>
				<li>
					Before trusting any browser screenshot, check
					<span class="inline-code">data-gosx-scene3d-backend</span>
					on the mount element. If it reads
					<span class="inline-code">canvas</span>
					, no shader, no post effect (post-FX), and no graphics processing unit (GPU) pipeline ran — the capture proves nothing. Use
					<span class="inline-code">gosx visual --require-backend</span>
					so this check runs automatically. See
					<a href="#enforce-backend-in-captures" class="inline-link">Enforce a Backend in Captures</a>
					and
					<a href="#common-traps" class="inline-link">Common Traps</a>
					.
				</li>
				<li>
					Check the mesh draw counters, not the mesh object count:
					<span class="inline-code">
						data-gosx-scene3d-webgpu-mesh-draw-calls
					</span>
					(submitted) versus
					<span class="inline-code">
						data-gosx-scene3d-webgpu-mesh-view-culled
					</span>
					(discarded by the CPU frustum cull). A nonzero
					<span class="inline-code">mesh-objects</span>
					count does not prove anything drew. See
					<a href="#state-and-draw-recorder" class="inline-link">State &amp; Draw Recorder</a>
					.
				</li>
				<li>
					Open the live registry in a running page:
					<span class="inline-code">
						window.__gosx_scene3d_debug.inspect(surfaceID)
					</span>
					for the full renderer, feature, and diagnostic snapshot of one mount. See
					<a href="#live-inspector" class="inline-link">Live Inspector</a>
					.
				</li>
				<li>
					If the bug only reproduces on real hardware — a compositor artifact under a real GPU that a software rasterizer will not show — drive the page with
					<span class="inline-code">scripts/windows-scene3d-probe.mjs</span>
					on a machine with a real GPU. See
					<a href="#compositor-diagnostics" class="inline-link">Compositor Diagnostics</a>
					.
				</li>
			</ol>
		</section>
		<section id="cpu-reference-renderer" class="docs-section-block">
			<h2>Tier 1 — CPU Reference Renderer</h2>
			<p>
				<span class="inline-code">gosx scene render</span>
				renders a scene entirely in Go: no browser, no GPU adapter, no display server, and no graphics driver. It walks
				<span class="inline-code">scene/preview</span>
				→
				<span class="inline-code">render/bundle</span>
				→
				<span class="inline-code">render/gpu/headless</span>
				, a CPU framebuffer implementation of the GPU device interface, and writes a PNG.
			</p>
			<p>
				<strong>
					This path skips the browser's JavaScript-side frustum cull.
				</strong>
				Use it as the definitive cross-check whenever an object exists in the scene graph but does not appear on screen. If the object paints here and not in the browser, the defect is in browser-side culling or camera math — not in scene authoring.
			</p>
			{CodeBlock("bash", `gosx scene render scene.json --out scene.png
	gosx scene render scene.json --out scene.png --width 1920 --height 1080 --time 1.5
	gosx scene render scene.json --fast --out thumb.png   # skip shadows/post-FX, cap tessellation`)}
			<p>
				The input is a bare SceneIR document or the runtime props JSON that
				<span class="inline-code">scene.Props</span>
				emits. Output includes object, batch, and material counts, frame time in milliseconds, and every native fallback diagnostic the render produced.
			</p>
			<p>
				For per-frame evidence inside a Go test — a PNG hash, pixel coverage percentage, visible pixel bounds, unique color count, and a pass/fail
				<span class="inline-code">Validate()</span>
				— use the
				<span class="inline-code">scene/harness</span>
				package directly:
			</p>
			{CodeBlock("go", `session := harness.New(props, preview.Options{Width: 320, Height: 240})
	if _, err := session.Render(0); err != nil {
	    t.Fatal(err)
	}
	if err := session.Validate(); err != nil {
	    t.Fatal(err) // fails on zero coverage, a device-lost frame, or a broken Selena material
	}
	session.WriteJSON(os.Stdout)`)}
			<p>
				A
				<span class="inline-code">Report</span>
				event with
				<span class="inline-code">Coverage: 0</span>
				and no
				<span class="inline-code">VisibleBounds</span>
				means the frame painted nothing at all — the same "geometry present, pixels absent" signature the browser-side cull bug produces, but caught in a unit test with no browser.
			</p>
		</section>
		<section id="state-and-draw-recorder" class="docs-section-block">
			<h2>Tier 2 — State &amp; Draw Recorder</h2>
			<p>
				Every mounted Scene3D surface publishes roughly 180
				<span class="inline-code">data-gosx-scene3d-webgpu-*</span>
				attributes on its mount element: points, particles, water, instanced meshes, post-FX, custom-material fallbacks, and the last error. Read them with
				<span class="inline-code">
					mount.getAttribute("data-gosx-scene3d-webgpu-mesh-draw-calls")
				</span>
				or through
				<span class="inline-code">window.__gosx_scene3d_telemetry(mount)</span>
				(see
				<a href="#live-inspector" class="inline-link">Live Inspector</a>
				).
			</p>
			<p>
				<strong>
					Mesh object counts are bundle counts, not draw counts.
				</strong>
				<span class="inline-code">data-gosx-scene3d-webgpu-mesh-objects</span>
				publishes the length of the authored mesh list unconditionally — an object the CPU frustum cull discarded still counts there. That ambiguity once let three mesh planes report a nonzero object count for two weeks while a camera-depth sign error culled them to zero pixels. Use the submitted-versus-culled pair instead:
			</p>
			<ul>
				<li>
					<span class="inline-code">
						data-gosx-scene3d-webgpu-mesh-draw-calls
					</span>
					— objects that were actually submitted to the GPU.
				</li>
				<li>
					<span class="inline-code">
						data-gosx-scene3d-webgpu-mesh-view-culled
					</span>
					— objects the CPU frustum cull discarded before they reached a draw call.
				</li>
			</ul>
			<p>
				<span class="inline-code">data-gosx-scene3d-cull-survivors</span>
				reports a different mechanism: GPU instanced-cull survivor counts for
				<span class="inline-code">InstancedMesh</span>
				nodes only, not the CPU
				<span class="inline-code">viewCulled</span>
				flag on ordinary mesh objects above. Enable it before mount:
			</p>
			{CodeBlock("javascript", `window.__gosx_scene3d_cull_telemetry = true;`)}
			<p>
				For a serialized scene file, check feature use, fallbacks, and backend capability with the CLI instead of a running page:
			</p>
			{CodeBlock("bash", `gosx scene check --strict --assets public --budget budget.json scene.json
	gosx scene inspect --json --strict --budget budget.json --assets public scene.json
	gosx scene validate --strict scene.json`)}
			<p>
				<span class="inline-code">gosx scene check</span>
				is the combined browser-free gate: validation, inspection and cost, asset reachability, CPU rendering, optional repeat determinism, and optional golden comparison produce one verdict.
				<span class="inline-code">gosx scene inspect</span>
				reports feature use, backend capability and degradation, estimated costs, asset reachability, and budget results without inventing a separate certification command.
			</p>
		</section>
		<section id="model-hydration-diagnostics" class="docs-section-block">
			<h2>Model Hydration Diagnostics</h2>
			<p>
				Model loading has two observable layers. The per-asset
				<span class="inline-code">gosx:scene3d:model-status</span>
				event reports
				<span class="inline-code">loading</span>
				,
				<span class="inline-code">loaded</span>
				,
				<span class="inline-code">cached</span>
				, or
				<span class="inline-code">error</span>
				along with the asset URL, cache state, generation, model ID and index, stage, and error. Its latest values are mirrored to
				<span class="inline-code">data-gosx-scene3d-model-status</span>
				,
				<span class="inline-code">-asset</span>
				,
				<span class="inline-code">-cache</span>
				,
				<span class="inline-code">-error</span>
				,
				<span class="inline-code">-generation</span>
				,
				<span class="inline-code">-id</span>
				, and
				<span class="inline-code">-stage</span>
				on the mount.
			</p>
			<p>
				The generation-level
				<span class="inline-code">gosx:scene3d:model-hydration-status</span>
				event reports
				<span class="inline-code">loading</span>
				,
				<span class="inline-code">committed</span>
				, or
				<span class="inline-code">failed</span>
				for the current transaction.
			</p>
			{CodeBlock("javascript", `const mount = document.querySelector("[data-gosx-scene3d-mounted]")
	mount.addEventListener("gosx:scene3d:model-hydration-status", ({ detail }) => {
	    console.log(detail.status, detail.generation, detail.counts, detail.error)
	})`)}
			<p>
				Its detail contains
				<span class="inline-code">status</span>
				,
				<span class="inline-code">generation</span>
				,
				<span class="inline-code">currentGeneration</span>
				,
				<span class="inline-code">committed</span>
				,
				<span class="inline-code">stale</span>
				,
				<span class="inline-code">stage</span>
				,
				<span class="inline-code">modelID</span>
				,
				<span class="inline-code">modelIndex</span>
				,
				<span class="inline-code">asset</span>
				,
				<span class="inline-code">error</span>
				, and counts for models, objects, points, labels, sprites, HTML entries, and lights.
			</p>
			<ul>
				<li>
					<span class="inline-code">loading</span>
					means one generation is staging all of its model-derived records.
				</li>
				<li>
					<span class="inline-code">committed</span>
					means the generation remained current and its staged records replaced the previous model-derived records in one commit. A network or parser error is reported by the per-asset event and can contribute an empty asset, so committed does not claim that every asset loaded successfully.
				</li>
				<li>
					<span class="inline-code">failed</span>
					means staging rolled back. The prior committed generation remains live; inspect
					<span class="inline-code">stage</span>
					and
					<span class="inline-code">error</span>
					for a declaration, fit, instantiation, or skin failure.
				</li>
				<li>
					A
					<span class="inline-code">stale</span>
					outcome means a newer command superseded the generation. Stale is returned to command orchestration as an outcome with
					<span class="inline-code">committed: false</span>
					and
					<span class="inline-code">stale: true</span>
					; it is not dispatched as a mount status event and cannot overwrite the newer generation's attributes. The runtime telemetry stream records
					<span class="inline-code">model-hydration-stale</span>
					with both generation numbers.
				</li>
			</ul>
			<p>
				The latest current-generation result is mirrored to
				<span class="inline-code">
					data-gosx-scene3d-model-hydration-status
				</span>
				,
				<span class="inline-code">-generation</span>
				,
				<span class="inline-code">-current-generation</span>
				,
				<span class="inline-code">-stale</span>
				,
				<span class="inline-code">-committed</span>
				,
				<span class="inline-code">-failure-stage</span>
				,
				<span class="inline-code">-error</span>
				, and
				<span class="inline-code">-counts</span>
				(the final field is JSON). Use the generation and committed fields together; an asset reporting
				<span class="inline-code">loaded</span>
				does not by itself prove that its generation won the transaction.
			</p>
			<h3>Mount-scoped texture variants</h3>
			<p>
				KTX2 and model texture selection uses the actual renderer attached to this mount: its backend, whether that renderer has a concrete KTX2 uploader, and its sorted capability-token set. A built block-compressed variant is selected only when both uploader readiness and the required tokens allow it; otherwise the authored image URI remains in use. This context is isolated per mount, so WebGL and WebGPU surfaces on the same page cannot leak texture choices into one another.
			</p>
			<p>
				Direct model loads and preload calls that do not have a mount context remain deliberately neutral. They do not infer support from page-global WebGPU state. External image URIs referenced by either a
				<span class="inline-code">.gltf</span>
				document or a GLB pass through the same texture-variant resolver.
			</p>
			<p>
				The runtime mirrors the renderer context and the latest per-model selection context to these mount attributes:
			</p>
			{CodeBlock("text", `data-gosx-scene3d-texture-variant-backend
	data-gosx-scene3d-texture-variant-upload-ready
	data-gosx-scene3d-texture-variant-token-count
	data-gosx-scene3d-texture-variant-scope
	data-gosx-scene3d-model-variant-scope
	data-gosx-scene3d-model-variant-backend
	data-gosx-scene3d-model-variant-upload-ready`)}
			<p>
				Each
				<span class="inline-code">gosx:scene3d:model-status</span>
				event also reports
				<span class="inline-code">variantScope</span>
				,
				<span class="inline-code">variantBackend</span>
				,
				<span class="inline-code">variantUploadReady</span>
				, and
				<span class="inline-code">variantTokenCount</span>
				in its detail.
			</p>
			<p>
				Treat the mount's
				<span class="inline-code">rendererDiagnostics</span>
				and these attributes as the source of truth. Page-scoped
				<span class="inline-code">pageCapabilities.webgpu</span>
				only describes the page probe and does not prove that a particular mount is using WebGPU or can upload the selected format.
			</p>
			<p>
				If a renderer swap changes the texture-variant fingerprint — backend, uploader readiness, or token set — the runtime creates a new scope and starts generation-fenced model rehydration. An older in-flight generation cannot commit over it. Parsed model cache entries are scope-qualified for the same isolation; the correctness tradeoff is that separate mounts can repeat fetch and parse work for the same model URL.
			</p>
		</section>
		<section id="live-inspector" class="docs-section-block">
			<h2>Tier 3 — Live Inspector</h2>
			<p>
				Every page with a mounted Scene3D surface exposes a debug registry at
				<span class="inline-code">window.__gosx_scene3d_debug</span>
				(schema
				<span class="inline-code">gosx.scene3d.debug.v1</span>
				). Open the browser console on a live page and call it directly:
			</p>
			{CodeBlock("javascript", `window.__gosx_scene3d_debug.listSurfaces();
	// [{ id, backend, ready, fallbackReason, counts, ... }, ...]

	window.__gosx_scene3d_debug.inspect("scene-mount");
	// renderer kind, fallbackReason, node counts, feature matrix,
	// camera, GPU resources, webgpuStats, renderer diagnostics

	window.__gosx_scene3d_debug.captureFrame("scene-mount");
	// { surfaceID, mimeType: "image/png", dataURL }

	window.__gosx_scene3d_debug.getDiagnostics("scene-mount");
	window.__gosx_scene3d_debug.getLastPick("scene-mount");`)}
			<p>
				The read-only telemetry helper supports legacy and explicitly scoped calls. It uses the debug registry and live mount handle when available, then combines that evidence with strictly parsed mount attributes:
			</p>
			{CodeBlock("javascript", `const legacy = window.__gosx_scene3d_telemetry(mount)
	const first = window.__gosx_scene3d_telemetry(null) // first mounted surface
	const one = window.__gosx_scene3d_telemetry({ scope: "mount", mount })
	const page = window.__gosx_scene3d_telemetry({ scope: "page" })`)}
			<p>
				Legacy
				<span class="inline-code">mount</span>
				and
				<span class="inline-code">null</span>
				calls return one mount snapshot; explicit mount scope returns the same shape. Page scope returns a page object with
				<span class="inline-code">scope</span>
				,
				<span class="inline-code">mountCount</span>
				,
				<span class="inline-code">mounts</span>
				,
				<span class="inline-code">diagnostics</span>
				, and
				<span class="inline-code">pageCapabilities</span>
				for every live registry mount. Its
				<span class="inline-code">diagnostics</span>
				array flattens every mount's diagnostics and tags each entry with
				<span class="inline-code">surfaceID</span>
				,
				<span class="inline-code">mountID</span>
				, and
				<span class="inline-code">engineID</span>
				so multi-surface failures remain attributable.
			</p>
			<p>
				<span class="inline-code">pageCapabilities.webgpu</span>
				is page-global probe evidence, and every mount snapshot labels it with
				<span class="inline-code">webgpuProbeScope: "page"</span>
				. The legacy
				<span class="inline-code">webgpu</span>
				field is an alias for that same page capability, not proof that this mount uses WebGPU.
				<span class="inline-code">rendererDiagnostics</span>
				is the per-mount renderer truth: a WebGL mount can therefore report a ready page-level WebGPU probe while its renderer diagnostics correctly identify WebGL.
			</p>
			<p>
				Boolean attributes accept only
				<span class="inline-code">true</span>
				or
				<span class="inline-code">false</span>
				, numeric attributes must be finite, and JSON telemetry such as cull survivors must be an object. Missing attributes stay
				<span class="inline-code">null</span>
				without a warning. Invalid values become
				<span class="inline-code">null</span>
				and add structured
				<span class="inline-code">scene.telemetry.invalid_attribute</span>
				diagnostics; malformed JSON adds
				<span class="inline-code">scene.telemetry.parse_error</span>
				. A failing registry snapshot, mount producer, or page capability probe is contained and reported as
				<span class="inline-code">scene.telemetry.snapshot_failed</span>
				with the producer and error in its diagnostic data.
			</p>
			<p>
				For an always-visible heads-up display instead of console calls, enable the on-page inspector overlay:
			</p>
			{CodeBlock("javascript", `window.__gosx_scene3d_inspector = true; // set before the Scene3D component mounts`)}
			{CodeBlock("gosx", `<Scene3D {...data.scene} inspector={true} />`)}
			<p>
				The overlay renders backend and fallback reason, render-loop state, viewport and device-pixel ratio, draw call and material counts, mesh and instance counts, HTML texture readiness, diagnostic and warning counts, and the last pick target — updated every frame, in the top-right corner of the mount.
			</p>
		</section>
		<section id="compositor-diagnostics" class="docs-section-block">
			<h2>Tier 4 — Compositor Diagnostics</h2>
			<p>
				<span class="inline-code">gosx perf &lt;url&gt; --trace out.json</span>
				captures a Chrome DevTools performance trace, including compositor threads, while sampling Scene3D frame stats. Load the file in
				<span class="inline-code">chrome://tracing</span>
				or the DevTools Performance panel to inspect compositor-thread activity frame by frame.
			</p>
			{CodeBlock("bash", `gosx perf http://localhost:8080/your-scene --frames 120 --trace trace.json`)}
			<p>
				<span class="inline-code">gosx perf</span>
				also detects a software rasterizer (SwiftShader, Mesa llvmpipe, Mesa softpipe, and others) and prints a banner above every GPU section of its report. Perf numbers under software rendering — frame budgets, shader compile stalls, buffer upload time — do not represent what a user on real hardware experiences. Treat any regression found only under software rendering as suspect until it is confirmed on real hardware.
			</p>
			<p>
				A GPU compositor bug can be unreproducible under SwiftShader or Mesa llvmpipe, because software compositing takes a different code path than the real hardware compositor. No sandboxed tool changes this — the fix is to drive the actual browser on the actual hardware. Run the render-truth probe on the machine that has the GPU. The common case takes no flags.
			</p>
			{CodeBlock("bash", `node scripts/windows-scene3d-probe.mjs`)}
			<p>
				That probes
				<span class="inline-code">https://m31labs.dev/</span>
				with Edge and writes
				<span class="inline-code">./scene3d-probe/</span>
				. The directory holds
				<span class="inline-code">report.json</span>
				(the full machine-readable dump),
				<span class="inline-code">summary.txt</span>
				(PASS or FAIL with reasons),
				<span class="inline-code">console.log</span>
				, and screenshots at three moments: initial paint, settle, and each caller-specified offset. Hand back the whole folder.
			</p>
			<p>
				The probe fails when the render is not GPU-backed, when an authored post effect never dispatched, when every submitted mesh or point entry drew nothing, when this browser's WGSL compiler reported an error, when the WebGPU device was lost during the run, or when a host-page guard latched on a backend that no longer runs. Each check is a defect that previously shipped while every health attribute read green.
			</p>
			<p>
				Options, all optional:
				<span class="inline-code">--url</span>
				(page to probe),
				<span class="inline-code">--browser edge|chrome|firefox</span>
				,
				<span class="inline-code">--out</span>
				(output directory),
				<span class="inline-code">--at 8,10,12</span>
				(extra capture offsets in seconds — the default targets the galaxy supernova window, which is dark outside 8-12s), and
				<span class="inline-code">--water</span>
				(the legacy
				<span class="inline-code">/demos/water</span>
				regression harness, now
				<span class="inline-code">scripts/windows-water-probe.mjs</span>
				).
			</p>
			<h3>Compare two browsers</h3>
			<p>
				Edge and Firefox do not share a WebGPU implementation. Edge uses Dawn, which translates WGSL through Tint. Firefox uses wgpu, which translates WGSL through naga. Selena validates its emitted WGSL with naga, so a shader can pass authoring-time validation and still hit a Tint bug in Edge. Run the probe twice and diff the two dumps; the schema is identical, so the comparison is mechanical.
			</p>
			{CodeBlock("bash", `node scripts/windows-scene3d-probe.mjs --browser edge    --out probe-edge
	node scripts/windows-scene3d-probe.mjs --browser firefox --out probe-firefox
	node scripts/windows-scene3d-probe.mjs --diff probe-edge probe-firefox`)}
			<p>
				Set
				<span class="inline-code">GOSX_BROWSER_EXECUTABLE</span>
				or
				<span class="inline-code">GOSX_PLAYWRIGHT_CORE</span>
				when the default Edge, Chrome or Playwright-core paths do not match the machine.
			</p>
		</section>
		<section id="render-truth-attributes" class="docs-section-block">
			<h2>Render-Truth Attributes</h2>
			<p>
				Most
				<span class="inline-code">data-gosx-scene3d-*</span>
				attributes report what was CONFIGURED or what the planner put in the bundle. The
				<span class="inline-code">data-gosx-scene3d-render-*</span>
				family reports what actually reached the framebuffer. Both renderers write the same names, so nothing that reads them has to branch on the backend. They appear only when the diagnostics tier is on — set
				<span class="inline-code">
					window.__gosx_scene3d_render_truth = true
				</span>
				before bootstrap, or
				<span class="inline-code">
					window.__gosx_telemetry_config.scene3dDiagnostics = true
				</span>
				. Production pays one boolean read.
			</p>
			<p>
				<span class="inline-code">render-post-chain</span>
				— one record per authored post effect, formatted
				<span class="inline-code">
					index:kind[@name]:pipelineState:dispatchCount
				</span>
				and pipe-separated. Pipeline state is
				<span class="inline-code">missing</span>
				,
				<span class="inline-code">pending</span>
				,
				<span class="inline-code">failed</span>
				or
				<span class="inline-code">ok</span>
				. An effect reading
				<span class="inline-code">ok</span>
				with
				<span class="inline-code">0</span>
				dispatches has a healthy pipeline that no pass ever bound. The companion counters are
				<span class="inline-code">render-post-authored</span>
				,
				<span class="inline-code">render-post-dispatched</span>
				,
				<span class="inline-code">render-post-dead</span>
				,
				<span class="inline-code">render-post-failed</span>
				and
				<span class="inline-code">render-post-pending</span>
				.
			</p>
			<p>
				<span class="inline-code">render-mesh-submitted</span>
				,
				<span class="inline-code">render-mesh-drawn</span>
				,
				<span class="inline-code">render-mesh-view-culled</span>
				and
				<span class="inline-code">render-mesh-undrawable</span>
				close the accounting identity
				<span class="inline-code">
					submitted = drawn + viewCulled + undrawable
				</span>
				. The point equivalents are
				<span class="inline-code">render-points-submitted</span>
				,
				<span class="inline-code">render-points-drawn</span>
				and the two
				<span class="inline-code">render-point-instances-*</span>
				counters.
			</p>
			<p>
				<span class="inline-code">render-uniform-time</span>
				is the reserved
				<span class="inline-code">time</span>
				auto-uniform exactly as fed to shaders, and
				<span class="inline-code">render-uniform-time-advancing</span>
				is 1 only when it grew since the previous publish. A frozen clock renders a static frame while every other attribute reads healthy.
			</p>
			<p>
				<span class="inline-code">render-backend-truth</span>
				is one JSON blob with the backend that ran, whether it is GPU-backed at all, the fallback reason, the adapter identity, the WGSL implementation (
				<span class="inline-code">dawn</span>
				or
				<span class="inline-code">wgpu</span>
				), device-loss state and the event journal.
				<span class="inline-code">render-events</span>
				is that journal on its own: an ordered, timestamped list of backend selections, fallbacks, device-loss events, uncaptured GPU errors and shader compiler complaints. A final state cannot describe a device that mounts healthy and dies eight seconds later; an ordered log can.
			</p>
			<p>
				<span class="inline-code">render-latches</span>
				and
				<span class="inline-code">render-stale-latches</span>
				report host-page decisions that settled on one backend and never re-armed. Register one with
				<span class="inline-code">
					window.__gosx_scene3d_render_truth_api.latch(name, backend, true)
				</span>
				. A latch whose recorded backend differs from the live backend is flagged stale — the signature of a guard that observed WebGPU, latched, and kept its decision after the device died.
			</p>
		</section>
		<section id="enforce-backend-in-captures" class="docs-section-block">
			<h2>Enforce a Backend in Captures</h2>
			<p>
				<span class="inline-code">gosx visual</span>
				accepts
				<span class="inline-code">--require-backend webgpu|webgl|any-gpu</span>
				. When set, it reads every
				<span class="inline-code">data-gosx-scene3d-backend</span>
				attribute on the page after the settle wait and hard-fails the capture — before any pixel comparison — if a mounted surface did not reach an acceptable backend, or if no Scene3D mount was found at all. The default (no flag) performs no check, so every existing caller keeps its current behavior.
			</p>
			{CodeBlock("bash", `gosx visual --require-backend webgpu http://localhost:8080/your-scene
	gosx visual --require-backend any-gpu --update http://localhost:8080/your-scene`)}
			<p>
				A failing run names every mount that did not qualify, states plainly that no shader or post effect (post-FX) ran when the backend fell back to the 2D canvas renderer, and prints the exact headless-Chrome flags that give the capture a real (software) GPU:
				<span class="inline-code">
					--use-gl=angle --use-angle=swiftshader --enable-unsafe-swiftshader --enable-webgl --ignore-gpu-blocklist
				</span>
				. Those flags are already built into
				<span class="inline-code">gosx visual</span>
				's own local Chrome launch; if the capture instead used
				<span class="inline-code">CHROME_WS_URL</span>
				(a remote or in-cluster headless-shell), confirm that service was started with the same flags — GoSX does not control a remote allocator's launch flags.
			</p>
			<p>
				Writing a custom capture harness instead of using
				<span class="inline-code">gosx visual</span>
				? Set
				<span class="inline-code">
					window.__gosx_scene3d_require_gpu = true
				</span>
				before the page mounts its Scene3D surface. It refuses only the Canvas2D fallback swap; a WebGL fallback still runs, because WebGL is a real GPU backend too.
			</p>
		</section>
		<section id="common-traps" class="docs-section-block">
			<h2>Common Traps</h2>
			<ul>
				<li>
					<strong>A canvas capture proves nothing.</strong>
					Headless Chrome without a GPU falls back to a plain 2D canvas renderer, silently. No shader, no post effect (post-FX), and no GPU geometry pipeline runs on that path, and the resulting image can still look plausible. Assert
					<span class="inline-code">data-gosx-scene3d-backend !== "canvas"</span>
					before trusting any capture, or pass
					<span class="inline-code">--require-backend</span>
					so
					<span class="inline-code">gosx visual</span>
					does it for you.
				</li>
				<li>
					<strong>
						A nonzero mesh count is not proof of a draw.
					</strong>
					<span class="inline-code">data-gosx-scene3d-webgpu-mesh-objects</span>
					is a bundle count — until recently, a culled object still counted there. Read
					<span class="inline-code">mesh-draw-calls</span>
					(submitted) and
					<span class="inline-code">mesh-view-culled</span>
					(discarded by the CPU frustum cull) to tell submitted geometry from discarded geometry.
				</li>
				<li>
					<strong>
						Software rasterizers hide real-hardware compositor bugs.
					</strong>
					SwiftShader and Mesa llvmpipe take a different compositing code path than a real GPU. A bug that will not reproduce under either one is not fixed — it needs
					<span class="inline-code">scripts/windows-scene3d-probe.mjs</span>
					on a machine with real hardware.
				</li>
			</ul>
		</section>
	</div>
}
