package docs

func Page() Node {
	return <div class="home">
		<section class="hero" aria-label="GoSX hero">
			<div class="hero__scene">
				<Scene3D {...data.heroScene} />
			</div>
			<div class="hero__overlay">
				<div class="hero__lockup">
					<p class="hero__kicker kicker">Pure Go &middot; Zero JS toolchain</p>
					<h1 class="hero__title chrome-text">GoSX</h1>
					<p class="hero__tagline">
						The Go-native web platform. One language, full stack.
					</p>
					<div class="hero__actions">
						<a href="/docs/getting-started" data-gosx-link="true" class="btn btn--gold">Get started</a>
						<a href="/demos" data-gosx-link="true" class="btn btn--ghost">Explore demos</a>
					</div>
				</div>
			</div>
			<div class="hero__scroll" aria-hidden="true">
				<span class="hero__scroll-indicator"></span>
			</div>
		</section>
		<section class="pitch" aria-label="What is GoSX">
			<div class="pitch__inner">
				<Each of={data.pitchStatements} as="statement">
					<div class="pitch__row reveal">
						<span class="pitch__num" aria-hidden="true">{statement.num}</span>
						<div>
							<h2 class="pitch__headline">{statement.headline}</h2>
							<p class="pitch__body">{statement.body}</p>
						</div>
					</div>
				</Each>
			</div>
		</section>
		<section class="showcases" aria-label="Capabilities">
			<Each of={data.showcases} as="showcase">
				<div class="showcase reveal" aria-label={showcase.title}>
					<div class="showcase__inner">
						<div class="showcase__text">
							<h2 class="showcase__title">{showcase.title}</h2>
							<p class="showcase__body">{showcase.body}</p>
							<a href={showcase.href} data-gosx-link="true" class="showcase__link">Deep dive</a>
						</div>
						<div class="showcase__demo">
							<div class="showcase__panel glass-panel">
								<span class="showcase__num chrome-text" aria-hidden="true">{showcase.num}</span>
								<span class="showcase__panel-tag">{showcase.title}</span>
							</div>
						</div>
					</div>
				</div>
			</Each>
		</section>
		<section class="proof" aria-label="By the numbers">
			<div class="proof__inner reveal" data-reveal-stagger="true">
				<h2 class="proof__heading">By the numbers</h2>
				<div class="proof__grid">
					<Each of={data.proofPoints} as="point">
						<div class="reveal">
							<StatCard value={point.value} label={point.label} />
						</div>
					</Each>
				</div>
			</div>
		</section>
	</div>
}
