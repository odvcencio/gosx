package docs

func Page() Node {
	return <section class="demos-landing" aria-labelledby="demos-landing-title">
		<header class="demos-landing__header">
			<p class="demos-landing__eyebrow">GoSX field notes · live in the browser</p>
			<h1 id="demos-landing-title" class="demos-landing__title">The framework is the demo.</h1>
			<p class="demos-landing__desc">
				Typed servers, realtime systems, simulations, and GPU scenes—each route is a working proof with its source and limitations in reach.
			</p>
			<ul class="demos-landing__facts" aria-label="Showcase principles">
				<li>
					<strong>Typed</strong>
					<span>Go to the browser</span>
				</li>
				<li>
					<strong>Native</strong>
					<span>SSR, hubs, and GPU</span>
				</li>
				<li>
					<strong>Honest</strong>
					<span>Fallbacks stay visible</span>
				</li>
			</ul>
		</header>
		<section
			class="demos-showreel"
			aria-labelledby="demos-showreel-title"
			aria-describedby="demos-showreel-description"
		>
			<div
				class="demos-showreel__canvas"
				role="group"
				aria-label="Interactive Scene3D orbital sculpture. Drag to orbit and scroll or pinch to zoom."
			>
				<Scene3D {...data.showreel} />
			</div>
			<div class="demos-showreel__overlay">
				<p class="demos-showreel__kicker">Scene3D · index study 01</p>
				<h2 id="demos-showreel-title" class="demos-showreel__title">Geometry, authored in Go.</h2>
				<p id="demos-showreel-description" class="demos-showreel__body">
					One bounded scene. Ten stable nodes. Procedural geometry only—no fetched models or textures.
				</p>
				<p class="demos-showreel__status">
					<span class="demos-showreel__status-dot" aria-hidden="true"></span>
					<span>
						<strong>Backend selected per mount</strong>
						· WebGPU → WebGL2 → Canvas2D / unsupported
					</span>
				</p>
				<p class="demos-showreel__controls">
					Drag to orbit · scroll or pinch to zoom
				</p>
				<div class="demos-showreel__actions">
					<a class="demos-button demos-button--primary" href="/demos/scene3d" data-gosx-link="true">
						Enter Geometry Zoo
						<span aria-hidden="true">↗</span>
					</a>
					<a
						class="demos-button demos-button--quiet"
						href={demoSourceURL("examples/gosx-docs/app/demos/showreel.go")}
						target="_blank"
						rel="noopener noreferrer"
					>
						View scene source
						<span aria-hidden="true">↗</span>
					</a>
				</div>
			</div>
		</section>
		<section class="demos-featured" aria-labelledby="demos-featured-title">
			<header class="demos-section-heading">
				<div>
					<p class="demos-section-heading__eyebrow">Four ways into Scene3D</p>
					<h2 id="demos-featured-title">Choose your depth.</h2>
				</div>
				<p>
					Rendering craft, simulation, gameplay, and measurement—without hiding which layer owns the work.
				</p>
			</header>
			<div class="demos-featured__list">
				<Each of={data.showcase} as="demo">
					<article class="demo-feature" data-demo={demo.Slug}>
						<header class="demo-feature__header">
							<span class={"demo-feature__chip demo-feature__chip--" + demo.Status}>
								{demo.Status}
							</span>
							<p class="demo-feature__tag">{demo.Tag}</p>
						</header>
						<h3 class="demo-feature__title">
							<a href={"/demos/" + demo.Slug} data-gosx-link="true">{demo.Title}</a>
						</h3>
						<p class="demo-feature__promise">{demo.Promise}</p>
						<p class="demo-feature__lesson">
							<span>GoSX lesson</span>
							{demo.Lesson}
						</p>
						<ul class="demo-feature__facets" aria-label={demo.Title + " capabilities"}>
							<Each of={demo.Facets} as="facet">
								<li>{facet}</li>
							</Each>
						</ul>
						<footer class="demo-feature__actions">
							<a href={"/demos/" + demo.Slug} data-gosx-link="true">
								Open demo
								<span aria-hidden="true">→</span>
							</a>
							<a href={demoSourceURL(demo.SourcePath)} target="_blank" rel="noopener noreferrer">
								Source
								<span aria-hidden="true">↗</span>
							</a>
							<Each of={demoGuides(demo.Slug)} as="guide">
								<a class="demo-feature__guide" href={guide.Href} data-gosx-link="true">{guide.Title} guide</a>
							</Each>
						</footer>
					</article>
				</Each>
			</div>
		</section>
		<section class="demos-more" aria-labelledby="demos-more-title">
			<header class="demos-section-heading demos-section-heading--compact">
				<div>
					<p class="demos-section-heading__eyebrow">More working proofs</p>
					<h2 id="demos-more-title">Beyond the canvas.</h2>
				</div>
			</header>
			<ul class="demos-more__list" role="list">
				<Each of={data.additional} as="demo">
					<li class="demo-row" data-demo={demo.Slug}>
						<a class="demo-row__main" href={"/demos/" + demo.Slug} data-gosx-link="true">
							<span class="demo-row__title">{demo.Title}</span>
							<span class="demo-row__tag">{demo.Tag}</span>
						</a>
						<span class={"demo-row__status demo-row__status--" + demo.Status}>{demo.Status}</span>
						<span class="demo-row__guides">
							<Each of={demoGuides(demo.Slug)} as="guide">
								<a class="demo-row__guide" href={guide.Href} data-gosx-link="true">{guide.Title} guide</a>
							</Each>
						</span>
						<a
							class="demo-row__source"
							href={demoSourceURL(demo.SourcePath)}
							target="_blank"
							rel="noopener noreferrer"
						>
							Source
							<span aria-hidden="true">↗</span>
						</a>
					</li>
				</Each>
			</ul>
		</section>
	</section>
}
