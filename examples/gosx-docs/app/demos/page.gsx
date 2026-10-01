package docs

func Page() Node {
	return <section class="demos-gallery" aria-labelledby="demos-gallery-title">
		<header class="demos-gallery__header">
			<p class="demos-gallery__eyebrow">GoSX demos</p>
			<h1 id="demos-gallery-title">Explore what you can build.</h1>
			<p>
				Try interactive scenes, shared systems, and server-rendered applications. Every card links to its source files and names the renderer the demo uses.
			</p>
		</header>
		<section class="demos-gallery__featured" aria-labelledby="demos-featured-title">
			<div class="demos-gallery__section-heading">
				<div>
					<p class="demos-gallery__eyebrow">Featured</p>
					<h2 id="demos-featured-title">Start here.</h2>
				</div>
				<p>
					Try the featured demo and follow its source into GoSX.
				</p>
			</div>
			<div class="demos-gallery__featured-list">
				<Each of={data.featured} as="demo">
					<article class="demo-card demo-card--featured" data-demo={demo.Slug}>
						<a
							class="demo-card__poster"
							href={"/demos/" + demo.Slug}
							data-gosx-link="true"
							aria-label={"Open " + demo.Title}
						>
							<img src={demo.PosterPath} alt="" width="960" height="600" decoding="async" fetchpriority="high" />
						</a>
						<div class="demo-card__body">
							<p class="demo-card__status">Featured</p>
							<h3 class="demo-card__title">
								<a href={"/demos/" + demo.Slug} data-gosx-link="true">{demo.Title}</a>
							</h3>
							<p class="demo-card__summary">{demo.Summary}</p>
							<ul class="demo-card__facets" aria-label={demo.Title + " capabilities"}>
								<Each of={demo.Facets} as="facet">
									<li>{facet}</li>
								</Each>
							</ul>
							<p class="demo-card__backends">
								{"Runs on " + demoBackendSummary(demo.Backends)}
							</p>
							<footer class="demo-card__actions">
								<a class="demo-card__open" href={"/demos/" + demo.Slug} data-gosx-link="true">Open demo</a>
								<a href={demoSourceURL(demo.SourcePath)} target="_blank" rel="noopener noreferrer">View source</a>
								<Each of={demoGuides(demo.Slug)} as="guide">
									<a class="demo-card__guide" href={guide.Href} data-gosx-link="true">
										{guide.Title + " guide"}
									</a>
								</Each>
								<details class="demo-card__sources">
									<summary>
										{demoSourceCountLabel(demo.SourcePaths)}
									</summary>
									<ul>
										<Each of={demo.SourcePaths} as="sourcePath">
											<li>
												<a href={demoSourceURL(sourcePath)} target="_blank" rel="noopener noreferrer">{sourcePath}</a>
											</li>
										</Each>
									</ul>
								</details>
							</footer>
						</div>
					</article>
				</Each>
			</div>
		</section>
		<Each of={data.groups} as="group">
			<section class="demos-gallery__group" aria-labelledby={"demo-group-" + group.ID}>
				<header class="demos-gallery__section-heading">
					<div>
						<p class="demos-gallery__eyebrow">Browse by topic</p>
						<h2 id={"demo-group-" + group.ID}>{group.Title}</h2>
					</div>
					<p>{group.Description}</p>
				</header>
				<div class="demos-gallery__grid">
					<Each of={group.Demos} as="demo">
						<article class="demo-card" data-demo={demo.Slug}>
							<a
								class="demo-card__poster"
								href={"/demos/" + demo.Slug}
								data-gosx-link="true"
								aria-label={"Open " + demo.Title}
							>
								<img src={demo.PosterPath} alt="" width="960" height="600" loading="lazy" decoding="async" />
							</a>
							<div class="demo-card__body">
								<p class="demo-card__status">{demoStatusLabel(demo.Status)}</p>
								<h3 class="demo-card__title">
									<a href={"/demos/" + demo.Slug} data-gosx-link="true">{demo.Title}</a>
								</h3>
								<p class="demo-card__summary">{demo.Summary}</p>
								<ul class="demo-card__facets" aria-label={demo.Title + " capabilities"}>
									<Each of={demo.Facets} as="facet">
										<li>{facet}</li>
									</Each>
								</ul>
								<p class="demo-card__backends">
									{"Runs on " + demoBackendSummary(demo.Backends)}
								</p>
								<footer class="demo-card__actions">
									<a class="demo-card__open" href={"/demos/" + demo.Slug} data-gosx-link="true">Open demo</a>
									<a href={demoSourceURL(demo.SourcePath)} target="_blank" rel="noopener noreferrer">View source</a>
									<Each of={demoGuides(demo.Slug)} as="guide">
										<a class="demo-card__guide" href={guide.Href} data-gosx-link="true">
											{guide.Title + " guide"}
										</a>
									</Each>
									<details class="demo-card__sources">
										<summary>
											{demoSourceCountLabel(demo.SourcePaths)}
										</summary>
										<ul>
											<Each of={demo.SourcePaths} as="sourcePath">
												<li>
													<a href={demoSourceURL(sourcePath)} target="_blank" rel="noopener noreferrer">{sourcePath}</a>
												</li>
											</Each>
										</ul>
									</details>
								</footer>
							</div>
						</article>
					</Each>
				</div>
			</section>
		</Each>
	</section>
}
