package docs

func Page() Node {
	return <div>
		<section class="doc-scene" aria-labelledby={docScene.HeadingID}>
			<div id={docScene.SurfaceID} class="doc-scene__surface">
				<Scene3D class="doc-scene__mount" {...docScene.Scene} respectReducedMotion={true}>
					<div class="doc-scene__fallback">{docScene.Scene.UnsupportedMessage}</div>
				</Scene3D>
			</div>
			<div class="doc-scene__teaching">
				<p class="doc-scene__eyebrow">{docScene.Eyebrow}</p>
				<p id={docScene.HeadingID} class="doc-scene__title" role="heading" aria-level="2">
					{docScene.Title}
				</p>
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
				<a href={docScene.DemoHref} data-gosx-link="true" class="doc-scene__link">
					{docScene.DemoLabel}
				</a>
			</div>
		</section>
		<section id="client-navigation">
			<h2>Opt-in client navigation</h2>
			<p>
				GoSX keeps ordinary links as the fallback. Add
				<span class="inline-code">data-gosx-link="true"</span>
				and install the navigation runtime to turn a same-origin HTTP(S) link into a managed transition.
			</p>
			{CodeBlock("gosx", `<a href="/docs/routing" data-gosx-link="true">Routing</a>`)}
			<p>
				Install the runtime once for the document. Applications built directly on
				<span class="inline-code">server.App</span>
				can call
				<span class="inline-code">app.EnableNavigation()</span>
				; a file-router layout can add
				<span class="inline-code">server.NavigationScript()</span>
				to its managed head.
			</p>
			{CodeBlock("go", `router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
	    ctx.AddHead(server.NavigationScript())
	    return server.HTMLDocument(ctx.Title("My App"), ctx.Head(), body)
	})`)}
		</section>
		<section id="page-transitions">
			<h2>What a managed transition owns</h2>
			<p>
				The runtime fetches a complete server-rendered document, parses it, disposes the outgoing page, replaces managed head and body content, loads the incoming managed scripts in role order, and bootstraps the new page. If managed navigation cannot safely continue, the native link remains the fallback.
			</p>
			<div class="feature-grid">
				<div class="card">
					<strong>Server HTML</strong>
					<p>
						The server still owns the next document. Navigation does not introduce a client router or duplicate route table.
					</p>
				</div>
				<div class="card">
					<strong>Managed ownership</strong>
					<p>
						Head markers, runtime roles, island manifests, engines, and hubs tell the host what to replace, retain, or dispose.
					</p>
				</div>
				<div class="card">
					<strong>Supersession</strong>
					<p>
						A newer navigation can supersede an older fetch. The runtime applies only the current transition.
					</p>
				</div>
			</div>
			<p>
				Use the public navigation facade for a programmatic transition or an explicit revalidation:
			</p>
			{CodeBlock("javascript", `await window.__gosx.navigation.navigate("/docs/routing")
	await window.__gosx.navigation.revalidate()
	console.log(window.__gosx.navigation.getState())`)}
		</section>
		<section id="lifecycle-scripts">
			<h2>Managed and lifecycle scripts</h2>
			<p>
				Register a page helper with
				<span class="inline-code">ctx.LifecycleScript</span>
				when it must be loaded in the lifecycle role after shared runtime assets and before the incoming bootstrap step. The script is a managed external asset; there is no module-exported
				<span class="inline-code">dispose()</span>
				contract.
			</p>
			{CodeBlock("go", `func Load(ctx *route.RouteContext, page route.FilePage) (any, error) {
	    ctx.LifecycleScript(server.AssetURL("scripts/chart-page.js"))
	    return map[string]any{"title": "Chart"}, nil
	}`)}
			<p>
				Use
				<span class="inline-code">ctx.ManagedScript</span>
				for an ordinary runtime-owned helper. Its explicit role and load mode become navigation metadata.
			</p>
			{CodeBlock("go", `ctx.ManagedScript(server.AssetURL("scripts/analytics.js"), server.ManagedScriptOptions{
	    Role: server.ManagedScriptRoleManaged,
	    Load: server.ManagedScriptLoadDOM,
	})`)}
		</section>
		<section id="prefetch">
			<h2>Prefetch and page cache</h2>
			<p>
				Managed links default to intent prefetch on pointer hover and keyboard focus. Render-time prefetch is explicit, reduced-data users are respected unless the author chooses
				<span class="inline-code">force</span>
				, and the cache entry expires after five minutes.
			</p>
			{CodeBlock("gosx", `<a href="/pricing" data-gosx-link="true" data-gosx-prefetch="render">Pricing</a>
	<a href="/account" data-gosx-link="true" data-gosx-prefetch="off">Account</a>
	<a href="/demo" data-gosx-link="true" data-gosx-prefetch="force">Demo</a>`)}
			<p>
				A fetched page can remove itself from the in-memory page cache with this document metadata:
			</p>
			{CodeBlock("html", `<meta name="gosx-page-cache" content="no-store">`)}
		</section>
		<section id="runtime-telemetry">
			<h2>Best-effort runtime telemetry</h2>
			<p>
				The public telemetry facade can emit events, inspect frozen transport accounting, and request a best-effort flush. A browser-accepted beacon is not proof that the server received it; a successful fetch response is the stronger acknowledgement represented by the snapshot.
			</p>
			{CodeBlock("javascript", `const telemetry = window.__gosx.telemetry
	telemetry.emit("info", "checkout", "dialog-opened", { orderID: "o_123" })
	telemetry.flush({ beacon: true })
	console.table(telemetry.snapshot())`)}
			<p>
				Set
				<span class="inline-code">
					window.__gosx_telemetry_config = &#123; enabled: false &#125;
				</span>
				before the runtime loads to disable browser collection. The public facade remains present for feature detection.
			</p>
		</section>
		<section id="disposal">
			<h2>Disposal is runtime-owned</h2>
			<p>
				Before swapping the page, GoSX runs its registered disposal path for mounted islands, engines, controllers, hubs, and other managed resources. Engine reuse is deliberately conservative and only carries a compatible mount into the next document. Application scripts should expose their own cleanup API when they allocate unmanaged browser resources; do not call private compatibility globals.
			</p>
		</section>
	</div>
}
