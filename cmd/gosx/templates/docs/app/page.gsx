package docs

func FeatureCard(props any) Node {
	return <section class="card">
		<strong>{props.Kicker}</strong>
		<h3>{props.Title}</h3>
		<p>{props.Body}</p>
		<div class="hero-actions">
			<Link class="hero-link" href={props.Href}>Open section</Link>
		</div>
		{props.Children}
	</section>
}

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Overview</span>
			<p class="lede">
				A paper-and-ink docs surface that exists to prove GoSX can route files, swap pages, and stay coherent.
			</p>
		</div>
		<h1>GoSX now has a docs site that actually runs through its own pipeline.</h1>
		<p>
			The point of this app is blunt: prove that GoSX can map a directory tree to routes, render real pages from
			<span class="inline-code">.gsx</span>
			source, and move between them without full reloads.
		</p>
		<div class="hero-actions">
			<Link class="hero-link primary" href="/docs/getting-started">Open the quickstart path</Link>
			<Link class="hero-link" href="/docs/routing">See file routing conventions</Link>
			<Link class="hero-link" href="/docs/forms">See forms and actions</Link>
			<Link class="hero-link" href="/docs/auth">See auth flow</Link>
		</div>
		<div class="hero-grid">
			<Each as="feature" of={data.features}>
				<FeatureCard {...feature}></FeatureCard>
			</Each>
		</div>
		<If when={data.showRouteTree}>
			<section class="callout">
				<strong>Route Tree</strong>
				<pre class="code-block">{`app/
  layout.gsx
  page.server.go
  page.gsx
  not-found.gsx
  docs/
    layout.gsx
    not-found.gsx
    forms/
      page.server.go
      page.gsx
    getting-started/
      page.server.go
      page.gsx
    images/
      page.server.go
      page.gsx
    routing/
      page.server.go
      page.gsx
    runtime/
      page.server.go
      page.gsx`}</pre>
			</section>
		</If>
		<section class="note-grid">
			<div class="note">
				<strong>Navigation</strong>
				<p>Marked links fetch HTML, replace managed head and body regions, then re-enter the GoSX runtime lifecycle.</p>
			</div>
			<div class="note">
				<strong>Rendering</strong>
				<p>
					The pages in this docs app are file-routed
					<span class="inline-code">.gsx</span>
					files with nested layout composition.
				</p>
			</div>
		</section>
	</article>
}
