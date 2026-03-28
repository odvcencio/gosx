package docs

func ShowcaseCard(props any) Node {
	return <section class={"showcase-card showcase-card--" + props.Tone}>
		<div class="showcase-card__surface">
			<div class="showcase-card__surface-head">
				<span class="eyebrow">{props.Kicker}</span>
				<span class="showcase-card__route">{props.Route}</span>
			</div>
			<div class="showcase-card__cues">
				<Each as="cue" of={props.Cues}>
					<span>{cue}</span>
				</Each>
			</div>
		</div>
		<h2>{props.Title}</h2>
		<p>{props.Body}</p>
		<ul class="showcase-card__points">
			<Each as="point" of={props.Points}>
				<li>{point}</li>
			</Each>
		</ul>
		<div class="hero-actions">
			<Link class="hero-link primary" href={props.Href}>{props.Label}</Link>
			<Link class="hero-link" href={props.SecondaryHref}>{props.SecondaryLabel}</Link>
		</div>
	</section>
}

func PrincipleCard(props any) Node {
	return <section class="card principle-card">
		<strong>{props.Kicker}</strong>
		<h3>{props.Title}</h3>
		<p>{props.Body}</p>
	</section>
}

func ProofChip(props any) Node {
	return <article class="proof-chip">
		<strong>{props.Value}</strong>
		<p>{props.Label}</p>
	</article>
}

func ApplicationRoute(props any) Node {
	return <article class="application-route">
		<span class="application-route__dot"></span>
		<div class="application-route__body">
			<div class="application-route__top">
				<strong>{props.Label}</strong>
				<span class="showcase-card__route">{props.Path}</span>
			</div>
			<p>{props.Body}</p>
		</div>
	</article>
}

func Page() Node {
	return <article class="home-shell">
		<section class="home-stage">
			<section class="home-hero">
				<div class="page-topper">
					<span class="eyebrow">Go-native web framework</span>
					<p class="lede">Build the site, the docs, and the demos in the same app you ship.</p>
				</div>
				<h1>Build in Go. Ship the site, the editor, and the 3D demo together.</h1>
				<p class="home-hero__body">
					This example is a real GoSX app, not a brochure hung next to one. Routes, server actions, auth, client navigation, and Scene3D all live in the same
					codebase.
				</p>
				<div class="hero-actions">
					<Link class="hero-link primary" href="/docs/getting-started">Start with the quickstart</Link>
					<Link class="hero-link" href="/demos/cms">Open the CMS demo</Link>
					<Link class="hero-link" href="/demos/scene3d">Open the 3D route</Link>
				</div>
				<section class="callout home-callout">
					<strong>Source of truth</strong>
					<p>
						Everything on this site comes from
						<span class="inline-code">examples/gosx-docs</span>
						. If a claim sounds good, you can open the file that proves it.
					</p>
				</section>
				<div class="home-proof">
					<Each as="proof" of={data.proofs}>
						<ProofChip {...proof}></ProofChip>
					</Each>
				</div>
			</section>
			<aside class="home-rail">
				<section class="hero-panel">
					<div class="hero-panel__head">
						<span class="eyebrow">Application map</span>
						<h2>One app. Four kinds of pages.</h2>
						<p>The same repo handles docs, editing flows, interactive runtime, and gated routes.</p>
					</div>
					<div class="application-route-list">
						<Each as="entry" of={data.routes}>
							<ApplicationRoute {...entry}></ApplicationRoute>
						</Each>
					</div>
				</section>
				<section class="hero-panel hero-panel--stack">
					<div class="hero-panel__head">
						<span class="eyebrow">What ships together</span>
						<h2>The pitch, the docs, and the demos all come from the same place.</h2>
						<p>You are looking at marketing, documentation, forms, auth, and runtime work without a second frontend stack glued on top.</p>
					</div>
					<div class="stack-tags">
						<Each as="item" of={data.stack}>
							<span class="stack-tag">{item}</span>
						</Each>
					</div>
				</section>
			</aside>
		</section>
		<section class="showcase-grid">
			<Each as="showcase" of={data.showcases}>
				<ShowcaseCard {...showcase}></ShowcaseCard>
			</Each>
		</section>
		<section class="platform-strip">
			<div class="platform-strip__intro">
				<span class="eyebrow">Why it feels cleaner</span>
				<h2>Start with pages. Add richer behavior only when it earns its place.</h2>
				<p class="lede">
					GoSX stays understandable because the default is plain HTML and Go. Interactivity is explicit. Routes stay in charge.
				</p>
			</div>
			<div class="feature-grid">
				<Each as="principle" of={data.principles}>
					<PrincipleCard {...principle}></PrincipleCard>
				</Each>
			</div>
		</section>
	</article>
}
