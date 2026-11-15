package docs

type FeatureCardProps struct {
	Kicker string
	Title  string
	Body   string
	Href   string
}

component FeatureCard(props: FeatureCardProps) {
	return <section class="card">
		<strong>{props.Kicker}</strong>
		<h3>{props.Title}</h3>
		<p>{props.Body}</p>
		<div class="hero-actions">
			<a class="hero-link" href={props.Href} data-gosx-link>Open section</a>
		</div>
	</section>
}

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Overview</span>
			<p class="lede">
				A starter documentation site. Replace these pages with your own.
			</p>
		</div>
		<h1>
			Write your docs as plain .gsx pages.
		</h1>
		<p>
			This site maps a directory tree to routes, renders each page from
			<span class="inline-code">.gsx</span>
			source on the server, and moves between pages without full reloads. Start from the quickstart, then edit the files under
			<span class="inline-code">app/</span>.
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
				<pre class="code-block">
					{`app/
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
		      page.gsx`}
				</pre>
			</section>
		</If>
		<section class="note-grid">
			<div class="note">
				<strong>Navigation</strong>
				<p>
					Marked links fetch HTML, replace managed head and body regions, then re-enter the GoSX runtime lifecycle.
				</p>
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
