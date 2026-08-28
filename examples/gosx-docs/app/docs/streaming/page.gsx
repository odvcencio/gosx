package docs

func Page() Node {
	return <article class="prose">
		<section class="doc-scene" aria-labelledby={docScene.HeadingID}>
			<div id={docScene.SurfaceID} class="doc-scene__surface">
				<Scene3D class="doc-scene__mount" {...docScene.Scene} respectReducedMotion={true}>
					<div class="doc-scene__fallback">{docScene.Scene.UnsupportedMessage}</div>
				</Scene3D>
			</div>
			<div class="doc-scene__teaching">
				<p class="doc-scene__eyebrow">{docScene.Eyebrow}</p>
				<p id={docScene.HeadingID} class="doc-scene__title" role="heading" aria-level="2">{docScene.Title}</p>
				<p class="doc-scene__summary">{docScene.Summary}</p>
				<dl class="doc-scene__facts">
					<div>
						<dt>Backend contract</dt>
						<dd>{docScene.BackendTruth}</dd>
					</div>
					<div>
						<dt>Interaction</dt>
						<dd>{docScene.InteractionHint}</dd>
					</div>
				</dl>
				<a href={docScene.DemoHref} data-gosx-link="true" class="doc-scene__link">{docScene.DemoLabel}</a>
			</div>
		</section>
		<div class="page-topper">
			<span class="eyebrow">Progressive server response</span>
			<p class="lede">
				Deferred boundaries render useful fallback HTML in the shell, then resolve concurrently and stream replacement templates as their server work completes.
			</p>
		</div>
		<h2 id="deferred-regions">Deferred regions</h2>
		<CodeBlock lang="go" source={data.deferSample} />
		<p>
			<span class="inline-code">ctx.Defer</span>
			registers a page-scoped resolver and returns its placeholder node.
			<span class="inline-code">ctx.Suspense</span>
			has the same completion behavior but marks the placeholder as a component boundary for tooling.
		</p>
		<CodeBlock lang="go" source={data.optionsSample} />
		<p>
			Options can choose a stable ID, placeholder tag, class, and boundary label. IDs must remain unique in the rendered document.
		</p>
		<h2 id="fallback-content">Fallback content</h2>
		<p>
			The fallback is real server-rendered HTML, not an empty client-only marker. Make it useful and accessible: reserve layout space, label loading state, and avoid hiding content required to understand the page.
		</p>
		{streamDemo}
		<h2 id="completion-order">Shell flush and completion order</h2>
		<p>
			When the response writer supports
			<span class="inline-code">http.Flusher</span>
			, GoSX writes and flushes the initial shell, runs registered resolvers in separate goroutines, and writes each chunk in completion order. A slow region does not hold a faster sibling behind it.
		</p>
		<p>
			Each chunk contains a template and a small replacement script. The runtime facade performs the managed fragment replacement when available, with a native DOM replacement fallback. Go's HTTP server and middleware determine the actual transfer framing; applications should not depend on a hard-coded
			<span class="inline-code">Transfer-Encoding</span>
			claim.
		</p>
		<h2 id="errors-and-csp">Errors and CSP</h2>
		<p>
			If a resolver returns an error or panics, GoSX streams a
			<span class="inline-code">data-gosx-stream-error</span>
			block instead of abandoning the remaining boundaries. Keep user-facing errors sanitized; the default block includes the returned error text.
		</p>
		<p>
			Under a nonce-based Content Security Policy, streamed scripts receive the same request nonce as the document. Custom response code must carry the page's deferred registry and nonce into
			<span class="inline-code">server.WriteHTML</span>
			.
		</p>
		<h2 id="caching">Caching</h2>
		<p>
			A streamed response is incomplete when cache headers are decided, so it does not receive the same body-derived validator path as a fully buffered page. Shared-cache policy also removes per-request nonce and request identity from the representation.
		</p>
		<p>
			Do not mark personalized deferred output publicly cacheable. Choose cache policy from the complete authorization and representation model, not merely from the shell.
		</p>
	</article>
}
