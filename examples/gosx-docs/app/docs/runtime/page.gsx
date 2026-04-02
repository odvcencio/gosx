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
		<section class="runtime-watch" data-runtime-watch>
			<div class="runtime-watch-copy">
				<span class="eyebrow">Lifecycle Script</span>
				<h2>
					Page-owned scripts can ride the navigation lifecycle instead of being bolted onto the DOM by hand.
				</h2>
				<p>
					This panel is updated by
					<span class="inline-code">runtime/watch-transport.js</span>
					, a page-owned lifecycle script registered from the route loader. It stays under the GoSX document contract, survives client transitions, and rebinds itself when you come back to this route.
				</p>
			</div>
			<div class="runtime-watch-grid">
				<div class="runtime-watch-card">
					<span class="runtime-watch-label">Last sync</span>
					<strong class="runtime-watch-value" data-runtime-watch-field="trigger">loading</strong>
				</div>
				<div class="runtime-watch-card">
					<span class="runtime-watch-label">Page pattern</span>
					<strong class="runtime-watch-value" data-runtime-watch-field="page">pending</strong>
				</div>
				<div class="runtime-watch-card">
					<span class="runtime-watch-label">Path</span>
					<strong class="runtime-watch-value" data-runtime-watch-field="path">pending</strong>
				</div>
				<div class="runtime-watch-card">
					<span class="runtime-watch-label">Navigation</span>
					<strong class="runtime-watch-value" data-runtime-watch-field="navigation">pending</strong>
				</div>
				<div class="runtime-watch-card">
					<span class="runtime-watch-label">Bootstrap mode</span>
					<strong class="runtime-watch-value" data-runtime-watch-field="bootstrap">pending</strong>
				</div>
				<div class="runtime-watch-card">
					<span class="runtime-watch-label">Head scripts</span>
					<strong class="runtime-watch-value" data-runtime-watch-field="scripts">0</strong>
				</div>
			</div>
			<p class="runtime-watch-note">
				Use
				<span class="inline-code">ManagedScript</span>
				for page-owned helpers that only need to stay managed across transitions. Use
				<span class="inline-code">LifecycleScript</span>
				when the script must be present before GoSX re-enters the next page and calls its bootstrap hooks.
			</p>
		</section>
		<section class="scene-callout">
			<div class="scene-copy">
				<span class="eyebrow">Native Scene3D</span>
				<h2>
					.gsx can now mount engine-backed surfaces and feed live runtime signals without dropping down to bare Go nodes.
				</h2>
				<p>
					The scene below is declared in
					<span class="inline-code">page.gsx</span>
					while
					<span class="inline-code">page.server.go</span>
					points it at a real shared-runtime engine program served by the app. That keeps the authoring path server-first while making the browser runtime explicit instead of ad hoc.
				</p>
				<div class="scene-controls">
					<div class="scene-control">
						<kbd>Pointer</kbd>
						<p>
							Move across the surface to steer the camera and pull the geometry off center.
						</p>
					</div>
					<div class="scene-control">
						<div class="scene-keyset">
							<kbd>Left</kbd>
							<kbd>Right</kbd>
						</div>
						<p>
							Bias the spin direction and shift the secondary mesh tint through shared keyboard signals.
						</p>
					</div>
					<div class="scene-control">
						<kbd>Up</kbd>
						<p>
							Warm the palette and tighten the camera to prove the runtime is ticking, not just hydrating once.
						</p>
					</div>
				</div>
			</div>
			<Scene3D class="scene-shell" {...data.sceneDemo}>
				<div class="scene-fallback">Preparing the scene runtime...</div>
			</Scene3D>
		</section>
		{DocsCodeBlock("javascript", `window.__gosx_dispose_page()
		window.__gosx_bootstrap_page()
		window.__gosx_page_nav.navigate("/docs/routing")`)}
		{DocsCodeBlock("go", `func Load(ctx *route.RouteContext, page route.FilePage) (any, error) {
    ctx.LifecycleScript(docsapp.PublicAssetURL("runtime/watch-transport.js"))
    return data, nil
}`)}
		{DocsCodeBlock("go", `ctx.ManagedScript(
    docsapp.PublicAssetURL("cms-demo.js"),
    server.ManagedScriptOptions{},
)`)}
		{DocsCodeBlock("gosx", `func Page() Node {
		    return <Scene3D class="scene-shell" programRef="/api/runtime/scene-program" {...data.sceneDemo}>
		        <div class="scene-fallback">Preparing the scene runtime...</div>
		    </Scene3D>
		}`)}
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
