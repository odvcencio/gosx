package docs

func Page() Node {
	return <section class="demos-landing" aria-labelledby="demos-landing-title">
		<header class="demos-landing__header">
			<p class="demos-landing__eyebrow">
				The GoSX demo studio
				<span>/ 12 working studies</span>
			</p>
			<h1 id="demos-landing-title" class="demos-landing__title">Build something you can feel.</h1>
			<p class="demos-landing__desc">
				Explore scenes, simulations, and interfaces built with GoSX. Turn a world, change a system, or open the source behind it.
			</p>
			<ul class="demos-landing__facts" aria-label="Showcase principles">
				<li>
					<strong>12</strong>
					<span>Working demos</span>
				</li>
				<li>
					<strong>Go</strong>
					<span>Source included</span>
				</li>
				<li>
					<strong>Live</strong>
					<span>Browser interaction</span>
				</li>
			</ul>
		</header>
		<section
			class="demos-showreel"
			aria-labelledby="demos-showreel-title"
			aria-describedby="demos-showreel-description"
		>
			<div class="demos-showreel__art" aria-hidden="true">
				<span></span>
			</div>
			<div class="demos-showreel__poster" role="img" aria-label="Illustration of an orbital sculpture"></div>
			<div class="demos-showreel__overlay">
				<p class="demos-showreel__kicker">Featured study / Scene3D</p>
				<h2 id="demos-showreel-title" class="demos-showreel__title">A scene you can turn.</h2>
				<p id="demos-showreel-description" class="demos-showreel__body">
					An orbital sculpture made from typed Go scene data. Follow the light around its form.
				</p>
				<div class="demos-showreel__actions">
					<a class="demos-button demos-button--primary" href="/demos/showreel" data-gosx-link="true">
						Turn the sculpture
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
					<p class="demos-section-heading__eyebrow">Start here</p>
					<h2 id="demos-featured-title">Four ways in.</h2>
				</div>
				<p>
					Explore a world, shape water, play a game, or measure the renderer.
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
								<a class="demo-feature__guide" href={guide.Href} data-gosx-link="true">
									{guide.Title + " guide"}
								</a>
							</Each>
						</footer>
					</article>
				</Each>
			</div>
		</section>
		<section class="demos-more" aria-labelledby="demos-more-title">
			<header class="demos-section-heading demos-section-heading--compact">
				<div>
					<p class="demos-section-heading__eyebrow">Keep exploring</p>
					<h2 id="demos-more-title">The rest of the studio.</h2>
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
								<a class="demo-row__guide" href={guide.Href} data-gosx-link="true">
									{guide.Title + " guide"}
								</a>
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
