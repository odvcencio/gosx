package docs

func Page() Node {
	return <div>
		<section id="preloads">
			<h2>Scene-driven preloads</h2>
			<p>
				Scene3D starts downloading its first resources while the browser parses the page. You do not need app JavaScript or hand-written preload links.
			</p>
			<p>
				The server reads each engine's scene data and emits script preloads for the WebGPU and WebGL2 backend candidates. A forced WebGL scene preloads WebGL2. A scene whose backend verdict allows only WebGPU preloads WebGPU. When both backends are candidates, neither is preloaded: the browser chooses its backend at runtime and downloads only that renderer, so a WebGPU page does not pay for the WebGL2 renderer.
			</p>
			<p>
				Scenes with models preload the glTF chunk. Authored animation, compute particles or instanced meshes, and compressed arrays or generated points preload their corresponding chunks. Image-based lighting products and authored KTX2 textures also preload the glTF chunk that carries the KTX2 reader. Shared scene programs preload the command chunk. Pages without Scene3D receive no Scene3D preloads.
			</p>
			<p>
				Asset hints are bounded: the first model per scene, the first material in each node class, scene-wide environment lighting, and the first water system's tile and cube-map images. Progressive models preload the preview, leaving the full model for its later load. Repeated URLs share a hint. Request destinations and anonymous CORS match the runtime loaders so downloads can be reused.
			</p>
			<p>
				The server cannot discover textures or external buffers inside a model before the model arrives. Those resources still load through the model loader.
			</p>
		</section>
		<section id="deferred">
			<h2>Deferred scenes</h2>
			<p>
				Set
				<span class="inline-code">scene.Props.Defer</span>
				to mount a scene after first paint. The page paints the mount's poster or fallback children first. The browser downloads no scene chunk, props or model until two animation frames and an idle callback have passed.
			</p>
			<p>
				<span class="inline-code">Until</span>
				is
				<span class="inline-code">"idle"</span>
				(the default) or
				<span class="inline-code">"load"</span>
				, which also waits for the window load event.
				<span class="inline-code">DelayMS</span>
				adds a wait before the idle step, and
				<span class="inline-code">TimeoutMS</span>
				caps the idle wait (1800 ms by default).
				<span class="inline-code">Inline</span>
				keeps the props in the page manifest; use it for a static export, which has no server for the props URL.
			</p>
			<p>
				The server keeps the props out of the page. It stores them by content hash and serves them at
				<span class="inline-code">/gosx/scene3d/&lt;hash&gt;.json</span>
				with an immutable cache header, so the browser and a CDN can cache them. The store lives in server memory and holds the most recent 64 scenes. After a restart, a cached page can point at a hash the server no longer has; the mount then reports
				<span class="inline-code">failed</span>
				and the poster stays.
			</p>
			<p>
				When the deferred load starts, the runtime emits the preload hints for the scene chunk, the one renderer chunk the browser will use, and the scene's first assets. It then fetches the props, inserts the scene chunk tag and mounts. The mount element reports its progress in
				<span class="inline-code">data-gosx-scene3d-defer-state</span>
				: scheduled, loading, mounting, mounted, gated or failed.
			</p>
			<p>
				<span class="inline-code">Variants</span>
				give a scene another props snapshot for a media query, such as a smaller scene for phones. The first matching query wins, and the server stores each variant under its own hash.
			</p>
			<p>
				Pages that use selective runtime bundles now receive the feature chunk script tags from the server. An app no longer writes its own tags for the engines, hubs, controllers or islands chunks.
			</p>
		</section>
		<section id="gates">
			<h2>Capability gates</h2>
			<p>
				List environment gates in
				<span class="inline-code">RequiredCapabilities</span>
				. The runtime checks them before it downloads any scene byte.
			</p>
			<p>
				<span class="inline-code">hardware-gpu</span>
				needs WebGL2 with no major performance caveat and a renderer that is not a software rasterizer.
				<span class="inline-code">full-motion</span>
				needs
				<span class="inline-code">prefers-reduced-motion</span>
				to be anything but
				<span class="inline-code">reduce</span>
				.
				<span class="inline-code">full-data</span>
				needs no Save-Data request, no
				<span class="inline-code">prefers-reduced-data</span>
				and no 2G-class connection.
			</p>
			<p>
				When a gate fails, nothing downloads and the poster stays. A deferred scene reports
				<span class="inline-code">gated</span>
				. A scene that is not deferred but has a gate loads its chunk only after the gate passes, which adds one serial step to its first load.
			</p>
		</section>
		<section id="renderer">
			<h2>The published renderer</h2>
			<p>
				A deferred or gated scene publishes the renderer it mounted on, and a failed gate publishes the refusal, on
				<span class="inline-code">&lt;html data-gosx-renderer&gt;</span>
				as
				<span class="inline-code">webgpu</span>
				,
				<span class="inline-code">webgl2</span>
				,
				<span class="inline-code">canvas2d</span>
				or
				<span class="inline-code">none</span>
				. The reason is in
				<span class="inline-code">data-gosx-renderer-reason</span>
				. The same value is the
				<span class="inline-code">$gosx.renderer</span>
				signal, and a
				<span class="inline-code">gosx:renderer</span>
				event fires when it changes.
			</p>
			<p>
				The scene code for this lives in a small chunk that loads only on pages with a deferred or gated scene; a scene with neither pays nothing for it. A page that has no such scene can ask for the answer with
				<span class="inline-code">RequireFeature("renderer-probe")</span>
				. That small chunk loads only on pages that require it. Style from the answer in CSS, for example
				<span class="inline-code">html[data-gosx-renderer="webgpu"] .answer-webgpu {"{"} display: inline {"}"}</span>
				.
			</p>
		</section>
		<section id="shaders">
			<h2>Parallel shader compilation</h2>
			<p>
				When WebGL2 exposes
				<span class="inline-code">KHR_parallel_shader_compile</span>
				, the runtime submits shader compilation and program linking without waiting for status queries. It polls completion across frames, then caches uniform and attribute locations. Scene draws wait until the submitted programs are ready, including static scenes and programs introduced by later scene updates.
			</p>
			<p>
				The queue covers base, skinned, custom and instanced PBR, crowd and crowd-motion variants, Selena, builtin and authored points, shadows, sky, post effects, water simulation and rendering, and legacy line and surface programs. Instanced, points and Selena programs are created only when the scene reaches those paths. Browsers without the extension retain synchronous compilation.
			</p>
		</section>
		<section id="resolution">
			<h2>Phone canvas resolution</h2>
			<p>
				A coarse-pointer display whose short side is at most 600 CSS pixels defaults to a maximum canvas device pixel ratio (DPR) of 1.5. Screen dimensions also cover landscape orientation. CSS size stays the same; the backing canvas uses fewer pixels.
			</p>
			<p>
				Set
				<span class="inline-code">scene.Props.MaxDevicePixelRatio</span>
				to override the automatic phone cap. For example,
				<span class="inline-code">MaxDevicePixelRatio: 2</span>
				permits DPR 2 on a capable phone. The engine prop is
				<span class="inline-code">maxDevicePixelRatio</span>
				. Capability limits, adaptive quality and
				<span class="inline-code">MaxPixels</span>
				still apply, so an override sets a ceiling rather than a guaranteed resolution. A display with a lower native DPR keeps that lower ratio.
			</p>
		</section>
		<section id="measurement">
			<h2>Measure the first frame</h2>
			<p>
				Use a mid-range phone with a cold cache. Record navigation to the first complete 3D frame, the chunk and asset request waterfall, shader compilation main-thread time, and the canvas backing dimensions. Repeat with WebGPU, forced WebGL2 and fallback, and with the shader extension unavailable. Check model, water and post-effect scenes for visual parity at the same authored DPR.
			</p>
			<p>
				These mechanisms reduce serial loading and rendering work. Their effect on first-frame time and Largest Contentful Paint depends on the scene, device, network and browser; measure both separately.
			</p>
		</section>
	</div>
}
