package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Error</span>
			<p class="lede">The router can discover a file-based error page and keep failures inside the docs shell.</p>
		</div>
		<h1>This docs request failed before it could render cleanly.</h1>
		<p>If this shows up in practice, the render path or the source file should be treated as broken.</p>
		<div class="hero-actions">
			<a href="/" data-gosx-link class="cta-link primary">Return home</a>
		</div>
	</article>
}
