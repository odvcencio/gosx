package docs

func FeatureCard(props any) Node {
	return <section class="card">
		<strong>{props.Kicker}</strong>
		<h3>{props.Title}</h3>
		<p>{props.Body}</p>
		<div class="hero-actions">
			<Link class="hero-link" href={props.Href}>Read guide</Link>
		</div>
		{props.Children}
	</section>
}

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Product</span>
			<p class="lede">
				GoSX is a Go-native web framework for shipping modern web apps without hand-assembling half a stack first.
			</p>
		</div>
		<h1>Build the app in Go. Keep it server-first. Add interactivity where it matters.</h1>
		<p>
			GoSX brings routing, layouts, server data loading, forms, auth, sessions, APIs, assets, and selective runtime into one product. Start from simple
			server-rendered pages, then layer in navigation, islands, streaming, or richer browser surfaces only when the experience calls for them.
		</p>
		<div class="hero-actions">
			<Link class="hero-link primary" href="/docs/getting-started">Start with the quickstart</Link>
			<Link class="hero-link" href="/docs/routing">See routing and layouts</Link>
			<Link class="hero-link" href="/docs/forms">See forms and actions</Link>
			<Link class="hero-link" href="/docs/runtime">See runtime and interactivity</Link>
		</div>
		<div class="hero-grid">
			<Each as="feature" of={data.features}>
				<FeatureCard {...feature}></FeatureCard>
			</Each>
		</div>
		<section class="callout">
			<strong>What ships in the box</strong>
			<p>
				One framework for pages, layouts, APIs, mutations, sessions, auth, assets, navigation, and deployable output. The goal is a single app model you can
				grow with, not a pile of unrelated tools taped together.
			</p>
		</section>
		<section class="note-grid">
			<div class="note">
				<strong>Server-first by default</strong>
				<p>Most pages can stay simple, fast, and easy to reason about. You do not need to turn the whole product into a client app to get a modern UX.</p>
			</div>
			<div class="note">
				<strong>Selective runtime</strong>
				<p>When the UI needs more, GoSX layers in navigation, islands, streaming regions, and richer browser-owned surfaces without replacing the core app model.</p>
			</div>
			<div class="note">
				<strong>Built for real apps</strong>
				<p>Routes, writes, auth, and deployment all sit in the same system, so the path from prototype to production stays coherent.</p>
			</div>
		</section>
	</article>
}
