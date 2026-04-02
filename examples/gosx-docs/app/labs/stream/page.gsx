package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Streaming</span>
			<p class="lede">
				Deferred regions flush fallback HTML first, then stream resolved content into place.
			</p>
		</div>
		<h1>
			Streaming in GoSX starts with deferred regions, not a separate rendering stack.
		</h1>
		<p>
			A page can flush its shell immediately, keep the fallback visible, and stream late sections into the live DOM as resolvers finish.
		</p>
		<section class="feature-grid">
			{streamRegion}
			<div class="card">
				<strong>API</strong>
				<p>
					Use ctx.Defer(...) or ctx.DeferWithOptions(...) inside server or route handlers.
				</p>
			</div>
		</section>
		{DocsCodeBlock("gosx", `ctx.Defer(
	    <p>Loading...</p>,
	    func() (gosx.Node, error) {
	        return <section>Resolved</section>, nil
	    },
	)`)}
		<div class="hero-actions">
			<Link class="cta-link" href="/docs/runtime">Back to runtime</Link>
			<Link class="cta-link primary" href="/">Back to overview</Link>
		</div>
	</article>
}
