package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Runtime</span>
			<p class="lede">
				Hydration bootstrap, page disposal, and script re-entry cooperate during client-side transitions.
			</p>
		</div>
		<h1>
			Page transitions reuse the runtime instead of pretending the browser does not exist.
		</h1>
		<p>
			When you click a marked link, GoSX fetches the next HTML document, swaps the managed head and body regions, loads any page-owned runtime scripts it needs, and re-runs the page bootstrap hook. Islands, engines, and hubs get a disposal phase before the next page claims the DOM.
		</p>
		<section class="feature-grid">
			<div class="card">
				<strong>Dispose</strong>
				<p>
					The current page tears down islands, engines, and hub sockets before replacement.
				</p>
			</div>
			<div class="card">
				<strong>Swap</strong>
				<p>
					Managed head nodes and body markup are replaced without reloading the whole tab.
				</p>
			</div>
			<div class="card">
				<strong>Re-enter</strong>
				<p>
					The existing bootstrap runtime hydrates whatever the next page needs.
				</p>
			</div>
			<div class="card">
				<strong>Prefetch</strong>
				<p>
					Hover and focus prefetch the next HTML payload so the click path is shorter.
				</p>
			</div>
		</section>
		<section class="scene-callout">
			<div class="scene-copy">
				<span class="eyebrow">Native Scene3D</span>
				<h2>
					.gsx can now mount engine-backed surfaces without dropping down to bare Go nodes.
				</h2>
				<p>
					The scene below is declared in
					<span class="inline-code">page.gsx</span>
					and its object graph comes from
					<span class="inline-code">page.server.go</span>
					. That keeps the authoring path server-first while still letting the browser own a live surface.
				</p>
			</div>
			<Scene3D class="scene-shell" {...data.sceneDemo}>
				<div class="scene-fallback">Preparing the scene runtime...</div>
			</Scene3D>
		</section>
		<pre class="code-block">
			{`window.__gosx_dispose_page()
		window.__gosx_bootstrap_page()
		window.__gosx_page_nav.navigate("/docs/routing")`}
		</pre>
		<pre class="code-block">
			{`func Page() Node {
		    return <Scene3D class="scene-shell" {...data.sceneDemo}>
		        <div class="scene-fallback">Preparing the scene runtime...</div>
		    </Scene3D>
		}`}
		</pre>
		<section class="callout">
			<strong>Constraint</strong>
			<p>
				This is still HTML-first. Engines extend the page with owned browser surfaces, but the server still shapes the route, data, and outer document.
			</p>
		</section>
		<div class="hero-actions">
			<Link class="cta-link" href="/docs/routing">Back to routing</Link>
			<Link class="cta-link primary" href="/">Back to overview</Link>
		</div>
	</article>
}
