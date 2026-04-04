package docs

func Page() Node {
	return <div class="home">
		<section class="hero" aria-label="GoSX hero">
			<div class="hero__scene">
				<Scene3D {...data.heroScene} />
			</div>
			<div class="hero__overlay">
				<h1 class="hero__title chrome-text">GoSX</h1>
				<p class="hero__tagline">The Go-native web platform</p>
			</div>
			<div class="hero__scroll" aria-hidden="true">
				<span class="hero__scroll-indicator"></span>
			</div>
		</section>
		<section class="pitch light" aria-label="What is GoSX">
			<div class="pitch__inner">
				<Each of={data.pitchStatements} as="statement">
					<div class="pitch__row reveal">
						<h2 class="pitch__headline">{statement.headline}</h2>
						<p class="pitch__body">{statement.body}</p>
					</div>
				</Each>
			</div>
		</section>
		<section class="showcases" aria-label="Capabilities">
			<Each of={data.showcases} as="showcase" index="i">
				<div class={"showcase " + showcase.mode + " reveal"} aria-label={showcase.title}>
					<div class="showcase__inner">
						<div class="showcase__text">
							<h2 class="showcase__title">{showcase.title}</h2>
							<p class="showcase__body">{showcase.body}</p>
							<a href={showcase.href} data-gosx-link="true" class="showcase__link">Deep dive</a>
						</div>
						<div class="showcase__demo">
							<div class="showcase__demo-placeholder glass-panel">
								<span class="showcase__demo-label">{showcase.title}</span>
							</div>
						</div>
					</div>
				</div>
			</Each>
		</section>
		<section class="proof light" aria-label="By the numbers">
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
