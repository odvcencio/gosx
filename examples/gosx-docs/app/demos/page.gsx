package docs

func Page() Node {
	return <section class="demos-landing" aria-label="Demos index">
		<header class="demos-landing__header">
			<h1 class="demos-landing__title">Demos</h1>
			<p class="demos-landing__desc">
				A tour of GoSX capabilities — servers, islands, real-time, simulation, and 3D.
			</p>
		</header>
		<div class="demos-landing__grid">
			<Each of={data.demos} as="demo">
				<If cond={demo.Live}>
					<a class="demo-card demo-card--live" href={"/demos/" + demo.Slug} data-demo={demo.Slug}>
						<div class="demo-card__swatch" style={"background: " + demo.Accent}></div>
						<div class="demo-card__meta">
							<span class="demo-card__chip demo-card__chip--live">Live</span>
							<h2 class="demo-card__title">{demo.Title}</h2>
							<p class="demo-card__tag">{demo.Tag}</p>
							<p class="demo-card__body">{demo.Body}</p>
						</div>
					</a>
				</If>
				<If cond={!demo.Live}>
					<div class="demo-card demo-card--soon" data-demo={demo.Slug} aria-disabled="true">
						<div class="demo-card__swatch" style={"background: " + demo.Accent}></div>
						<div class="demo-card__meta">
							<span class="demo-card__chip demo-card__chip--soon">Soon</span>
							<h2 class="demo-card__title">{demo.Title}</h2>
							<p class="demo-card__tag">{demo.Tag}</p>
							<p class="demo-card__body">{demo.Body}</p>
						</div>
					</div>
				</If>
			</Each>
		</div>
	</section>
}
