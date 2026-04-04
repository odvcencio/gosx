package docs

func Page() Node {
	return <div>
		<section id="client-navigation">
			<h2>Client Navigation</h2>
			<p>
				GoSX ships an opt-in client navigation model. Mark a link with
				<span class="inline-code">data-gosx-link</span>
				and the runtime intercepts the click, fetches the next document, and swaps the managed
				regions of the page without a full browser reload.
			</p>
			{CodeBlock("gosx", `<a href="/docs/routing" data-gosx-link="true" class="nav-link">Routing</a>`)}
			<p>
				The navigation script is injected by
				<span class="inline-code">server.NavigationScript()</span>
				from the layout loader. It must be present in the document head before any
				<span class="inline-code">data-gosx-link</span>
				attribute is encountered.
			</p>
			{CodeBlock("go", `router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
    ctx.AddHead(server.NavigationScript())
    return server.HTMLDocument(ctx.Title("My App"), ctx.Head(), body)
})`)}
		</section>

		<section id="page-transitions">
			<h2>Page Transitions</h2>
			<p>
				When the runtime intercepts a link click it follows a predictable sequence: dispose the
				current page, fetch the next HTML document, swap the managed head and body regions,
				then re-run the bootstrap hook for the incoming page.
			</p>
			<div class="feature-grid">
				<div class="card">
					<strong>Fetch</strong>
					<p>The next document is fetched as a full server-rendered HTML response.</p>
				</div>
				<div class="card">
					<strong>Dispose</strong>
					<p>Islands, engines, and hub sockets on the current page receive a teardown signal.</p>
				</div>
				<div class="card">
					<strong>Swap</strong>
					<p>Managed head nodes and the body markup are replaced in place.</p>
				</div>
				<div class="card">
					<strong>Bootstrap</strong>
					<p>The shared runtime re-hydrates whatever the incoming page declares.</p>
				</div>
			</div>
			<p>
				The navigation can also be triggered programmatically from a lifecycle script:
			</p>
			{CodeBlock("javascript", `window.__gosx_page_nav.navigate("/docs/routing")
window.__gosx_dispose_page()
window.__gosx_bootstrap_page()`)}
		</section>

		<section id="lifecycle-scripts">
			<h2>Lifecycle Scripts</h2>
			<p>
				Page-owned scripts can participate in the navigation lifecycle instead of being wired
				to the DOM manually. Register a lifecycle script from the route loader using
				<span class="inline-code">ctx.LifecycleScript</span>.
				It will be loaded before the next page bootstraps and re-executed on each navigation
				back to the route.
			</p>
			{CodeBlock("go", `func Load(ctx *route.RouteContext, page route.FilePage) (any, error) {
    ctx.LifecycleScript(server.AssetURL("my-page-script.js"))
    return data, nil
}`)}
			<p>
				Use
				<span class="inline-code">data-gosx-lifecycle-script</span>
				in markup when the script is rendered inline by the template rather than registered
				from the loader.
			</p>
			{CodeBlock("gosx", `<script
    src={server.AssetURL("chart-init.js")}
    data-gosx-lifecycle-script
></script>`)}
			<p>
				Lifecycle scripts differ from managed scripts:
				<span class="inline-code">LifecycleScript</span>
				guarantees execution before the bootstrap hooks run on the next page.
				<span class="inline-code">ManagedScript</span>
				is appropriate for helpers that only need to stay present across transitions.
			</p>
			{CodeBlock("go", `ctx.ManagedScript(
    server.AssetURL("analytics.js"),
    server.ManagedScriptOptions{},
)`)}
		</section>

		<section id="prefetch">
			<h2>Prefetch</h2>
			<p>
				The runtime prefetches the next document on link hover and focus so the navigation
				path is shorter for mouse and keyboard users. No configuration is required; the
				behavior is active whenever
				<span class="inline-code">server.NavigationScript()</span>
				is present.
			</p>
			<p>
				Prefetch fires a standard
				<span class="inline-code">fetch</span>
				request with the same headers the full navigation would use. The response is cached
				in memory for the duration of the hover or until the click resolves. Pages that must
				not be prefetched can opt out with a
				<span class="inline-code">Cache-Control: no-store</span>
				response header.
			</p>
		</section>

		<section id="disposal">
			<h2>Disposal</h2>
			<p>
				Before the incoming page is mounted, the current page enters a disposal phase.
				Islands stop their signal subscriptions, engines release GPU resources, and hub
				WebSocket connections are closed. Scripts registered as lifecycle scripts also
				receive a dispose callback if they export one.
			</p>
			{CodeBlock("javascript", `// In a lifecycle script
export function dispose() {
    myCanvas.getContext("webgl2")?.getExtension("WEBGL_lose_context")?.loseContext()
    clearInterval(myTimer)
}`)}
			<p>
				Disposal is synchronous by default. Async teardown can be awaited by returning a
				promise from the
				<span class="inline-code">dispose</span>
				export. The runtime will wait up to 300 ms before proceeding with the swap.
			</p>
		</section>
	</div>
}
