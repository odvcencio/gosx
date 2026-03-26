package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Docs Missing</span>
			<p class="lede">
				This is the <span class="inline-code">/docs</span> scoped not-found page, not the site-wide fallback.
			</p>
		</div>
		<h1>The docs subtree could not resolve this page.</h1>
		<p>
			The request matched the docs section, so the router resolved the nearest directory-scoped
			<span class="inline-code">not-found.gsx</span>
			instead of falling back to the root 404 page.
		</p>
		<div class="hero-actions">
			<a href="/docs/getting-started" data-gosx-link class="cta-link primary">Open getting started</a>
			<a href="/docs/routing" data-gosx-link class="cta-link">Review routing conventions</a>
		</div>
	</article>
}
