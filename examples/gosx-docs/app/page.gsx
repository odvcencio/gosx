package docs

import (
	"m31labs.dev/gosx/browser"
	"m31labs.dev/gosx/signal"
)

func Page() Node {
	return <div class="home">
		<section class="home-hero" aria-labelledby="home-title">
			<div class="home-hero__media">
				<img
					class="home-hero__poster"
					src={data.heroPosterURL}
					alt="Six studio objects on a textured wood tabletop, backed by a soft gradient sweep"
					width="1100"
					height="760"
					fetchpriority="high"
					decoding="async"
				 />
				<Scene3D
					id="home-tabletop-scene"
					class="home-hero__scene"
					{...data.heroScene}
					startPolicy="idle-visible-hardware"
					autoRotate={false}
					maxFrameRate={30}
					respectReducedMotion={true}
					data-gosx-scene3d-reveal-class="home-scene-ready"
				>
					<p class="home-hero__fallback">
						The live scene starts when this page is idle and a hardware GPU is available.
					</p>
				</Scene3D>
				<p class="home-hero__caption">
					A shared scene, rendered from typed Go data.
				</p>
			</div>
			<div class="home-hero__copy">
				<p class="home-hero__eyebrow">Go from request to GPU</p>
				<h1 id="home-title">One Go codebase. The whole product.</h1>
				<p class="home-hero__tagline">
					Your server, realtime room, shared document, and browser UI run in Go. Scene3D renders on WebGPU or WebGL2, with no CGo or hand-written JavaScript.
				</p>
				<ul class="home-hero__facts" aria-label="What runs in GoSX">
					<li>Go server</li>
					<li>Hub + CRDT</li>
					<li>Go browser UI</li>
					<li>WebGPU / WebGL2</li>
				</ul>
				<div class="home-hero__quickstart" aria-labelledby="quickstart-title">
					<div class="home-hero__quickstart-head">
						<h2 id="quickstart-title">Try the quickstart</h2>
						<QuickstartCopy text={data.commands} />
					</div>
					<pre>
						<code>{data.commands}</code>
					</pre>
				</div>
				<nav class="home-hero__actions" aria-label="Get started">
					<a class="home-hero__start" href="/docs/getting-started" data-gosx-link="true">Start in 5 minutes</a>
					<a class="home-hero__open" href="/demos/tabletop" data-gosx-link="true">Open the shared scene</a>
				</nav>
			</div>
		</section>
		<section class="runtime-map" aria-labelledby="runtime-map-title">
			<div class="section-shell">
				<header
					class="section-heading"
					data-gosx-motion
					data-gosx-motion-trigger="view"
					data-gosx-motion-preset="slide-up"
				>
					<p class="kicker">The execution model</p>
					<h2 id="runtime-map-title">
						Five surfaces. Use only what the feature needs.
					</h2>
					<p>
						A GoSX page starts on the server. Interactivity, graphics, and shared state are explicit upgrades&mdash;not a runtime tax paid by every route.
					</p>
				</header>
				<ol class="runtime-map__list">
					<Each of={data.runtimeSurfaces} as="surface">
						<li
							class="runtime-card"
							data-gosx-motion
							data-gosx-motion-trigger="view"
							data-gosx-motion-preset="slide-up"
						>
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
				<div
					class="gpu-story__copy"
					data-gosx-motion
					data-gosx-motion-trigger="view"
					data-gosx-motion-preset="slide-up"
				>
					<p class="kicker">Scene3D + Selena</p>
					<h2 id="gpu-story-title">
						One scene graph. One shader source. Two GPU backends.
					</h2>
					<p>
						Scene3D lowers typed Go scenes into shared SceneIR. Selena compiles each authored
						<code>.sel</code>
						shader to WGSL for WebGPU and GLES for WebGL2. GoSX selects and drives the backend; the demo does not carry a second hand-written WebGL implementation.
					</p>
					<div class="gpu-story__actions">
						<a href="/demos/water" data-gosx-link="true" class="showcase__link">Run the water system</a>
						<a href="/docs/scene3d" data-gosx-link="true" class="showcase__link">Read the Scene3D model</a>
					</div>
				</div>
				<div
					class="pipeline"
					aria-label="GoSX graphics pipeline"
					data-gosx-motion
					data-gosx-motion-trigger="view"
					data-gosx-motion-preset="slide-up"
				>
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
					<footer class="pipeline__footer">
						One typed scene &middot; two GPU backends &middot; shared bindings
					</footer>
				</div>
			</div>
		</section>
		<section class="paths" aria-labelledby="paths-title">
			<div class="section-shell">
				<header
					class="section-heading"
					data-gosx-motion
					data-gosx-motion-trigger="view"
					data-gosx-motion-preset="slide-up"
				>
					<p class="kicker">Explore by outcome</p>
					<h2 id="paths-title">
						Start with the kind of system you are building.
					</h2>
				</header>
				<div class="paths__grid">
					<Each of={data.paths} as="path">
						<article
							class="path-card"
							data-gosx-motion
							data-gosx-motion-trigger="view"
							data-gosx-motion-preset="slide-up"
						>
							<span class="path-card__index" aria-hidden="true">{path.num}</span>
							<h3>{path.title}</h3>
							<p>{path.body}</p>
							<span class="path-card__tools">{path.tools}</span>
							<a href={path.href} data-gosx-link="true" class="path-card__link">
								Explore
								{path.title}
							</a>
						</article>
					</Each>
				</div>
			</div>
		</section>
		<section class="site-proof" aria-labelledby="site-proof-title">
			<div class="section-shell">
				<header class="section-heading">
					<p class="kicker">This site is the product</p>
					<h2 id="site-proof-title">
						Follow each surface into a working route.
					</h2>
					<p>
						The guides explain the contract. These production routes exercise it and link back to the exact source and deliberate limits.
					</p>
				</header>
				<div class="site-proof__grid">
					<Each of={data.siteProofs} as="proof">
						<article class="site-proof__card">
							<span>{proof.surface}</span>
							<h3>{proof.title}</h3>
							<p>{proof.body}</p>
							<a href={proof.href} data-gosx-link="true">{proof.cta}</a>
						</article>
					</Each>
				</div>
			</div>
		</section>
		<section class="proof" aria-labelledby="proof-title">
			<div
				class="proof__inner section-shell"
				data-gosx-motion
				data-gosx-motion-trigger="view"
				data-gosx-motion-preset="slide-up"
			>
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

//gosx:island
func QuickstartCopy(props QuickstartCopyProps) Node {
	status := signal.New("")
	copy := func() {
		status.Set(browser.ClipboardWrite(props.Text) ? "Commands copied" : "Clipboard unavailable. Select the commands to copy them.")
	}
	return <span class="quickstart-copy">
		<button type="button" aria-label="Copy quickstart commands" onClick={copy}>Copy</button>
		<span role="status" aria-live="polite">{status.Get()}</span>
	</span>
}
