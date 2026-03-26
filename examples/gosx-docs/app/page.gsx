package docs

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
			<a href="/docs/getting-started" data-gosx-link class="hero-link primary">Open the quickstart path</a>
			<a href="/docs/routing" data-gosx-link class="hero-link">See file routing conventions</a>
			<a href="/docs/forms" data-gosx-link class="hero-link">See forms and actions</a>
			<a href="/docs/auth" data-gosx-link class="hero-link">See auth flow</a>
		</div>
		<div class="hero-grid">
			<section class="card">
				<strong>Tier 1</strong>
				<h3>App scaffolding, metadata, APIs, 404 and 500 pages, public assets, and env loading.</h3>
			</section>
			<section class="card">
				<strong>Tier 2</strong>
				<h3>Client navigation and per-route middleware without spinning up a second framework.</h3>
			</section>
			<section class="card">
				<strong>Tier 3</strong>
				<h3>File-based pages, route groups, nested layouts, scoped 404s, redirects, and rewrites are now part of the platform story.</h3>
			</section>
			<section class="card">
				<strong>Phase 2</strong>
				<h3>Sessions, CSRF, flashed form state, and auth now ride inside the same routed app model.</h3>
			</section>
			<section class="card">
				<strong>Why this matters</strong>
				<h3>The docs tree itself is the route map. You can see the information architecture from the filesystem.</h3>
			</section>
		</div>
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
