package docs

func Page() Node {
	return <div class="home">
		<section class="hero" aria-labelledby="home-title">
			<div class="hero__scene" aria-hidden="true">
				<Scene3D {...data.heroScene} />
			</div>
			<div class="hero__overlay">
				<div class="hero__lockup">
					<p class="hero__kicker kicker">Go-authored from request to GPU</p>
					<h1 id="home-title" class="hero__title chrome-text">GoSX</h1>
					<p class="hero__tagline">Build server-rendered applications, interactive tools, realtime systems, and GPU scenes in Go&mdash;without a JavaScript app toolchain.</p>
					<div class="hero__actions">
						<a href="/docs/getting-started" data-gosx-link="true" class="btn btn--gold">Start with GoSX</a>
						<a href="/demos" data-gosx-link="true" class="btn btn--ghost">See it running</a>
					</div>
					<ul class="hero__facts" aria-label="Platform summary">
						<li>Server HTML</li>
						<li>Go/WASM islands</li>
						<li>WebGPU + WebGL2</li>
					</ul>
				</div>
			</div>
			<div class="hero__scroll" aria-hidden="true">
				<span class="hero__scroll-indicator"></span>
			</div>
		</section>

		<section class="runtime-map" aria-labelledby="runtime-map-title">
			<div class="section-shell">
				<header class="section-heading" data-gosx-motion data-gosx-motion-trigger="view" data-gosx-motion-preset="slide-up">
					<p class="kicker">The execution model</p>
					<h2 id="runtime-map-title">Five surfaces. Use only what the feature needs.</h2>
					<p>A GoSX page starts on the server. Interactivity, graphics, and shared state are explicit upgrades&mdash;not a runtime tax paid by every route.</p>
				</header>
				<ol class="runtime-map__list">
					<Each of={data.runtimeSurfaces} as="surface">
						<li class="runtime-card" data-gosx-motion data-gosx-motion-trigger="view" data-gosx-motion-preset="slide-up">
							<div class="runtime-card__head">
								<span class="runtime-card__num" aria-hidden="true">{surface.num}</span>
								<h3>{surface.name}</h3>
							</div>
							<p>{surface.purpose}</p>
							<span class="runtime-card__cost">{surface.cost}</span>
						</li>
					</Each>
				</ol>
			</div>
		</section>

		<section class="gpu-story" aria-labelledby="gpu-story-title">
			<div class="gpu-story__inner section-shell">
				<div class="gpu-story__copy" data-gosx-motion data-gosx-motion-trigger="view" data-gosx-motion-preset="slide-up">
					<p class="kicker">Scene3D + Selena</p>
					<h2 id="gpu-story-title">One scene graph. One shader source. Two GPU backends.</h2>
					<p>Scene3D lowers typed Go scenes into shared SceneIR. Selena compiles each authored <code>.sel</code> shader to WGSL for WebGPU and GLES for WebGL2. GoSX selects and drives the backend; the demo does not carry a second hand-written WebGL implementation.</p>
					<div class="gpu-story__actions">
						<a href="/demos/water" data-gosx-link="true" class="showcase__link">Run the water system</a>
						<a href="/docs/scene3d" data-gosx-link="true" class="showcase__link">Read the Scene3D model</a>
					</div>
				</div>
				<div class="pipeline" aria-label="GoSX graphics pipeline" data-gosx-motion data-gosx-motion-trigger="view" data-gosx-motion-preset="slide-up">
					<div class="pipeline__source">
						<span class="pipeline__label">Author once</span>
						<strong>Go scene + Selena shader</strong>
					</div>
					<div class="pipeline__spine" aria-hidden="true"></div>
					<div class="pipeline__outputs">
						<div>
							<span class="pipeline__label">Preferred</span>
							<strong>WebGPU</strong>
							<small>WGSL render + compute</small>
						</div>
						<div>
							<span class="pipeline__label">Compatible</span>
							<strong>WebGL2</strong>
							<small>GLES render + ping-pong</small>
						</div>
					</div>
					<footer class="pipeline__footer">Shared bindings &middot; native harness evidence &middot; no demo JavaScript</footer>
				</div>
			</div>
		</section>

		<section class="paths" aria-labelledby="paths-title">
			<div class="section-shell">
				<header class="section-heading" data-gosx-motion data-gosx-motion-trigger="view" data-gosx-motion-preset="slide-up">
					<p class="kicker">Explore by outcome</p>
					<h2 id="paths-title">Start with the kind of system you are building.</h2>
				</header>
				<div class="paths__grid">
					<Each of={data.paths} as="path">
						<article class="path-card" data-gosx-motion data-gosx-motion-trigger="view" data-gosx-motion-preset="slide-up">
							<span class="path-card__index" aria-hidden="true">{path.num}</span>
							<h3>{path.title}</h3>
							<p>{path.body}</p>
							<span class="path-card__tools">{path.tools}</span>
							<a href={path.href} data-gosx-link="true" class="path-card__link">Explore {path.title}</a>
						</article>
					</Each>
				</div>
			</div>
		</section>

		<section class="proof" aria-labelledby="proof-title">
			<div class="proof__inner section-shell" data-gosx-motion data-gosx-motion-trigger="view" data-gosx-motion-preset="slide-up">
				<div>
					<p class="kicker">The compact version</p>
					<h2 id="proof-title" class="proof__heading">A platform with explicit boundaries.</h2>
				</div>
				<div class="proof__grid">
					<Each of={data.proofPoints} as="point">
						<div data-gosx-motion data-gosx-motion-trigger="view" data-gosx-motion-preset="slide-up">
							<StatCard value={point.value} label={point.label} />
						</div>
					</Each>
				</div>
			</div>
		</section>
	</div>
}
