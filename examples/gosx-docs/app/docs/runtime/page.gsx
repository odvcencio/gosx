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
			<h2>Client Navigation</h2>
			<p>
				GoSX ships an opt-in client navigation model. Mark a link with
				<span class="inline-code">data-gosx-link</span>
				and the runtime intercepts the click, fetches the next document, and swaps the managed regions of the page without a full browser reload.
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
			<p>
				Programmatic
				<span class="inline-code">navigation.navigate(url)</span>
				soft-fetches only same-origin HTTP(S) documents. Safe cross-origin HTTP(S) targets use a normal hard navigation; non-HTTP schemes are rejected without fetching or changing the location.
			</p>
			<p>
				Call
				<span class="inline-code">window.__gosx.navigation.revalidate()</span>
				to invalidate cached HTML for the current URL and run one forced same-URL soft navigation. It returns a promise, replaces rather than pushes history, and preserves scroll by default. Fetch failures reject before the live document is replaced, so a caller may choose a hard-load fallback. The legacy
				<span class="inline-code">refresh()</span>
				and its
				<span class="inline-code">refreshState()</span>
				alias synchronously reapply navigation state without fetching.
			</p>
			{CodeBlock("javascript", `await window.__gosx.navigation.revalidate()

	// Opt into normal top-of-page scroll for this revalidation.
	await window.__gosx.navigation.revalidate({ preserveScroll: false })

	// Synchronous state-only compatibility operation; no network request.
	const state = window.__gosx.navigation.refresh()

	// Read-only counters: started advances on fetch; applied advances only
	// after that fetched page completes the managed navigation lifecycle.
	const { started, applied } = window.__gosx.navigation.getFetchEpoch()`)}
		</section>
		<section id="headless-controllers">
			<h2>Headless Controllers</h2>
			<p>
				A controller storage load can write decoded JSON directly into a shared signal, publish the legacy structured storage event through an output, or do both. A missing, empty, or invalid stored value does not overwrite the signal's typed island default; an output still receives an event with a null value.
			</p>
			{CodeBlock("go", `Storage: &controller.Storage{
	    Namespace: "workspace",
	    Load: []controller.StorageSlot{{
	        Key:    "sidebar-open",
	        Signal: "$sidebar.open", // decoded value directly
	        Output: "events",        // optional structured event too
	    }},
	}`)}
		</section>
		<section id="page-transitions">
			<h2>Page Transitions</h2>
			<p>
				When the runtime intercepts a link click it follows a predictable sequence: dispose the current page, fetch the next HTML document, swap the managed head and body regions, then re-run the bootstrap hook for the incoming page.
			</p>
			<div class="feature-grid">
				<div class="card">
					<strong>Fetch</strong>
					<p>
						The next document is fetched as a full server-rendered HTML response.
					</p>
				</div>
				<div class="card">
					<strong>Dispose</strong>
					<p>
						Islands, engines, and hub sockets on the current page receive a teardown signal.
					</p>
				</div>
				<div class="card">
					<strong>Swap</strong>
					<p>
						Managed head nodes and the body markup are replaced in place.
					</p>
				</div>
				<div class="card">
					<strong>Bootstrap</strong>
					<p>
						The shared runtime re-hydrates whatever the incoming page declares.
					</p>
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
				Page-owned scripts can participate in the navigation lifecycle instead of being wired to the DOM manually. Register a lifecycle script from the route loader using
				<span class="inline-code">ctx.LifecycleScript</span>
				. It will be loaded before the next page bootstraps and re-executed on each navigation back to the route.
			</p>
			{CodeBlock("go", `func Load(ctx *route.RouteContext, page route.FilePage) (any, error) {
	    ctx.LifecycleScript(server.AssetURL("my-page-script.js"))
	    return data, nil
	}`)}
			<p>
				Use
				<span class="inline-code">data-gosx-lifecycle-script</span>
				in markup when the script is rendered inline by the template rather than registered from the loader.
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
		<section id="runtime-telemetry">
			<h2>Runtime Telemetry</h2>
			<p>
				The browser runtime batches structured events for
				<span class="inline-code">/_gosx/client-events</span>
				. Transport errors never enter application control flow; inspect the read-only accounting snapshot when you need to know what the runtime queued, attempted, or could not send.
			</p>
			{CodeBlock("javascript", `const telemetry = window.__gosx.telemetry
	telemetry.emit("info", "checkout", "payment-dialog-opened", { orderID: "o_123" })
	console.table(telemetry.snapshot())`)}
			<p>
				<span class="inline-code">window.__gosx.telemetry.snapshot()</span>
				returns a frozen object. Read its counters as transport accounting:
			</p>
			<ul>
				<li>
					<span class="inline-code">enabled</span>
					and
					<span class="inline-code">session</span>
					report whether the client transport is active and its page-session ID.
					<span class="inline-code">emittedEvents</span>
					counts every emit call, including events later refused because the queue was full.
				</li>
				<li>
					<span class="inline-code">queueDepth</span>
					is the number of events still waiting in memory.
					<span class="inline-code">queueCapacity</span>
					and
					<span class="inline-code">batchCapacity</span>
					report the configured limits. An event already handed to fetch is no longer in the queue.
				</li>
				<li>
					<span class="inline-code">attemptedEvents</span>
					and
					<span class="inline-code">attemptedBatches</span>
					count batches removed from the queue for serialization.
					<span class="inline-code">dispatchedEvents</span>
					and
					<span class="inline-code">dispatchedBatches</span>
					count transport calls that were invoked successfully.
				</li>
				<li>
					<span class="inline-code">browserAcceptedEvents</span>
					and
					<span class="inline-code">browserAcceptedBatches</span>
					increase only when
					<span class="inline-code">navigator.sendBeacon()</span>
					returns true. That means the browser accepted the payload for delivery; it does not confirm that the server received or logged it.
				</li>
				<li>
					<span class="inline-code">serverAcceptedEvents</span>
					and
					<span class="inline-code">serverAcceptedBatches</span>
					increase only after a fetch request receives a successful HTTP response. Beacon delivery cannot contribute to these counters because the beacon API exposes no response.
				</li>
				<li>
					<span class="inline-code">droppedOverflowEvents</span>
					counts events refused when the queue is full.
					<span class="inline-code">droppedSerializationEvents</span>
					counts events that could not be normalized or serialized.
					<span class="inline-code">failedEvents</span>
					and
					<span class="inline-code">failedBatches</span>
					count the event and batch impact of terminal normalization, serialization, or fetch failures.
					<span class="inline-code">beaconFailures</span>
					and
					<span class="inline-code">fetchFailures</span>
					count failed transport attempts in batch units. A rejected beacon increments
					<span class="inline-code">beaconFailures</span>
					and falls back to fetch; it is terminal only if that fallback also fails.
				</li>
				<li>
					<span class="inline-code">pendingRequests</span>
					is the number of fetch promises still in flight. A zero queue with a nonzero pending count is dispatched, not settled.
					<span class="inline-code">lastFlushAt</span>
					,
					<span class="inline-code">lastFlushReason</span>
					,
					<span class="inline-code">lastFailureAt</span>
					, and
					<span class="inline-code">lastFailureReason</span>
					explain the most recent transition.
				</li>
			</ul>
			<p>
				Attempted batches are not automatically requeued after a terminal failure. The counters preserve that outcome so a quiet queue cannot be mistaken for confirmed delivery.
			</p>
			<h3>Drain before leaving the page</h3>
			<p>
				On
				<span class="inline-code">pagehide</span>
				and when the document becomes hidden, the runtime drains every queued batch up to the configured queue capacity. It prefers
				<span class="inline-code">sendBeacon()</span>
				and falls back to a keepalive fetch when beacon submission is unavailable, rejected, or throws. You can request the same behavior manually:
			</p>
			{CodeBlock("javascript", `window.__gosx.telemetry.flush({ drain: true })  // drain with keepalive fetch
	window.__gosx.telemetry.flush({ beacon: true }) // drain, preferring sendBeacon`)}
			<p>
				Flush is best-effort and does not return a delivery promise. Check
				<span class="inline-code">pendingRequests</span>
				for unsettled fetches, and do not treat a browser-accepted beacon as server confirmation.
			</p>
			<h3>Disable telemetry and bound ingestion</h3>
			<p>
				To disable browser telemetry, set the configuration before the runtime loads:
			</p>
			{CodeBlock("javascript", `window.__gosx_telemetry_config = { enabled: false }`)}
			<p>
				The public API remains available for feature detection:
				<span class="inline-code">telemetry.enabled</span>
				is false,
				<span class="inline-code">session()</span>
				returns an empty string, emit and flush are no-ops, and snapshot counters remain zero.
			</p>
			<p>
				The server switch is independent.
				<span class="inline-code">GOSX_TELEMETRY=off</span>
				(or
				<span class="inline-code">false</span>
				,
				<span class="inline-code">0</span>
				,
				<span class="inline-code">disabled</span>
				, or
				<span class="inline-code">no</span>
				) makes the built-in endpoint return 404 without logging events. Unless you also disable the browser, that response appears as a fetch failure in the snapshot.
			</p>
			<p>
				The built-in server limit is 60 events per minute per remote address, not 60 HTTP requests. A custom
				<span class="inline-code">ClientEventsHandler</span>
				can set
				<span class="inline-code">ClientEventsOptions.RatePerMin</span>
				; nonpositive values select the default. The server validates JSON first, caps a batch at 100 events, and then reserves quota for the accepted event count atomically. If the full count does not fit, it rejects the batch with 429, logs none of it, and does not consume partial quota.
			</p>
		</section>
		<section id="prefetch">
			<h2>Prefetch</h2>
			<p>
				The runtime prefetches the next document on link hover and focus so the navigation path is shorter for mouse and keyboard users. No configuration is required; the behavior is active whenever
				<span class="inline-code">server.NavigationScript()</span>
				is present.
			</p>
			<p>
				Prefetch fires a standard
				<span class="inline-code">fetch</span>
				request with the same headers the full navigation would use. The response is cached in memory for the duration of the hover or until the click resolves. Pages that must not be prefetched can opt out with a
				<span class="inline-code">Cache-Control: no-store</span>
				response header.
			</p>
		</section>
		<section id="disposal">
			<h2>Disposal</h2>
			<p>
				Before the incoming page is mounted, the current page enters a disposal phase. Islands stop their signal subscriptions, engines release GPU resources, and hub WebSocket connections are closed. Scripts registered as lifecycle scripts also receive a dispose callback if they export one.
			</p>
			{CodeBlock("javascript", `// In a lifecycle script
	export function dispose() {
	    myCanvas.getContext("webgl2")?.getExtension("WEBGL_lose_context")?.loseContext()
	    clearInterval(myTimer)
	}`)}
			<p>
				Disposal is synchronous by default. Async teardown can be awaited by returning a promise from the
				<span class="inline-code">dispose</span>
				export. The runtime will wait up to 300 ms before proceeding with the swap.
			</p>
		</section>
	</div>
}
