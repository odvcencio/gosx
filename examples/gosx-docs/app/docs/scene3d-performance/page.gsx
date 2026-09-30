package docs

func Page() Node {
	return <div>
		<section id="preloads">
			<h2>Scene-driven preloads</h2>
			<p>
				Scene3D starts downloading its first resources while the browser parses the page. You do not need app JavaScript or hand-written preload links.
			</p>
			<p>
				The server reads each engine's scene data and emits script preloads for the WebGPU and WebGL2 backend candidates. A forced WebGL scene preloads WebGL2. A scene whose backend verdict allows only WebGPU preloads WebGPU. When both backends are candidates, both are preloaded so fallback can start sooner.
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
