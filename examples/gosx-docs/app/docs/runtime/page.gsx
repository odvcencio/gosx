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
		<section id="periodic-revalidation">
			<h2>Periodic revalidation</h2>
			<p>
				Add
				<span class="inline-code">data-gosx-revalidate-interval</span>
				to the first matching element on a page. The runtime revalidates on that interval, with zero application JavaScript.
			</p>
			{CodeBlock("gosx", `<main data-gosx-revalidate-interval="4s" data-gosx-revalidate-src="/api/league/version">
	    <!-- draft room, scoreboard, or other live server state -->
	</main>`)}
			<p>
				The interval accepts whole seconds or whole minutes only, for example
				<span class="inline-code">"4s"</span>
				or
				<span class="inline-code">"2m"</span>
				, and must resolve to at least one second. An invalid interval logs one console warning and disables periodic revalidation for the page.
			</p>
			<p>
				Add
				<span class="inline-code">data-gosx-revalidate-src</span>
				to poll a same-origin JSON endpoint instead of revalidating on every tick. The runtime fetches it with
				<span class="inline-code">no-store</span>
				and keeps the last response body. It revalidates only when the body changes; the first poll records a baseline and never revalidates. Without
				<span class="inline-code">data-gosx-revalidate-src</span>
				, the runtime revalidates on every interval.
			</p>
			<p>
				A tick skips entirely, with no fetch and no revalidation, in these cases:
			</p>
			<ul>
				<li>The document is hidden.</li>
				<li>
					An input, textarea, or select has focus.
				</li>
				<li>
					A navigation or form submission is in flight.
				</li>
			</ul>
			<p>
				A cross-origin
				<span class="inline-code">data-gosx-revalidate-src</span>
				logs one console warning and disables periodic revalidation for the page. Fetch errors skip silently, and the next tick tries again.
			</p>
			<p>
				Teardown is automatic. A soft navigation to a page without the attribute clears the interval. A soft navigation to a page with the attribute reads it again.
			</p>
		</section>
		<section id="declarative-countdown">
			<h2>Declarative countdown</h2>
			<p>
				Add
				<span class="inline-code">data-gosx-countdown</span>
				with an RFC3339 instant to count down to that moment with zero application JavaScript. Write the element's initial text yourself, so the page shows a correct value even with no JavaScript at all. The runtime takes over at the first 1-second tick after the page loads, with one shared timer for every countdown on the page.
			</p>
			{CodeBlock("gosx", `<span data-gosx-countdown="2026-08-22T16:00:00-04:00"
	      data-gosx-countdown-format="mm:ss">15:00</span>`)}
			<p>
				To compute that initial text from the server's own clock instead of a hand-typed guess, format it in the page's loader and render the result as the element's text:
			</p>
			{CodeBlock("go", `func Load(ctx *route.RouteContext, page route.FilePage) (any, error) {
	    remaining := time.Until(launchAt)
	    return map[string]any{
	        "countdownText": fmt.Sprintf("%d:%02d", int(remaining.Minutes()), int(remaining.Seconds())%60),
	    }, nil
	}`)}
			{CodeBlock("gosx", `<span data-gosx-countdown="2026-08-22T16:00:00-04:00"
	      data-gosx-countdown-format="mm:ss">{data.countdownText}</span>`)}
			<p>
				Add
				<span class="inline-code">data-gosx-countdown-format</span>
				to render compact text on the countdown element itself:
				<span class="inline-code">"dhms"</span>
				for day/hour/minute/second text, or
				<span class="inline-code">"mm:ss"</span>
				for a minutes:seconds clock. On a target more than a day away,
				<span class="inline-code">"mm:ss"</span>
				renders the total minutes remaining rather than wrapping at 60 — for example a 2-day target shows a value like
				<span class="inline-code">"2880:00"</span>
				, not a clock that resets at each hour.
			</p>
			<p>
				To own the markup instead, leave the format off and mark child elements with
				<span class="inline-code">data-gosx-countdown-segment</span>
				, set to one of
				<span class="inline-code">"days"</span>
				,
				<span class="inline-code">"hours"</span>
				,
				<span class="inline-code">"minutes"</span>
				, or
				<span class="inline-code">"seconds"</span>
				. The runtime fills only the matching element, and leaves the rest of the subtree untouched. Write each segment's own initial value, the same way the compact form's initial text is author-written above.
			</p>
			{CodeBlock("gosx", `<div data-gosx-countdown="2026-08-22T16:00:00-04:00">
	    <b data-gosx-countdown-segment="days">3</b>
	    <b data-gosx-countdown-segment="hours">04</b>
	    <b data-gosx-countdown-segment="minutes">12</b>
	    <b data-gosx-countdown-segment="seconds">09</b>
	</div>`)}
			<p>
				A segment set missing one or more of the four names still renders, but each present segment shows only its own remainder modulo its own unit — a seconds-only segment on a 5 minute countdown shows
				<span class="inline-code">"59"</span>
				, not
				<span class="inline-code">"299"</span>
				. The runtime logs one console warning for an incomplete segment set.
			</p>
			<p>
				Add
				<span class="inline-code">data-gosx-countdown-warn="30s"</span>
				to add the class
				<span class="inline-code">gosx-countdown--warn</span>
				once the remaining time drops to the threshold or below. This is a small declarative duration subset, not a general Go duration parser: whole hour/minute/second components combined in one value, such as
				<span class="inline-code">"30s"</span>
				or
				<span class="inline-code">"1m30s"</span>
				, or a bare non-negative integer as whole seconds.
			</p>
			<p>
				Add
				<span class="inline-code">data-gosx-countdown-then="revalidate"</span>
				to fire one revalidation of the page's revalidate root (see
				<span class="inline-code">data-gosx-revalidate-interval</span>
				above) the first time the countdown reaches zero. It never fires a second time, and it does nothing while the page has no active revalidation poll — no
				<span class="inline-code">data-gosx-revalidate-interval</span>
				element, or one whose value failed validation.
			</p>
			<p>
				The countdown clamps a passed target to zero. It never shows a negative value. An invalid instant leaves the element exactly as it was written.
				<span class="inline-code">gosx check</span>
				rejects a static
				<span class="inline-code">data-gosx-countdown</span>
				value that is not a valid RFC3339 instant, a static
				<span class="inline-code">data-gosx-countdown-format</span>
				value outside
				<span class="inline-code">"dhms"</span>
				and
				<span class="inline-code">"mm:ss"</span>
				, a static
				<span class="inline-code">data-gosx-countdown-segment</span>
				value outside the four supported names, a static
				<span class="inline-code">data-gosx-countdown-warn</span>
				value outside the duration subset above, and a static
				<span class="inline-code">data-gosx-countdown-then</span>
				value other than
				<span class="inline-code">"revalidate"</span>
				, before the page ever serves.
			</p>
		</section>
		<section id="declarative-reorder">
			<h2>Declarative reorder</h2>
			<p>
				Add
				<span class="inline-code">data-gosx-reorder</span>
				to a container and
				<span class="inline-code">data-gosx-reorder-item</span>
				to each direct child that can move, set to that item's identity. Mark one descendant
				<span class="inline-code">data-gosx-reorder-handle</span>
				— the item itself is the handle when none is marked. Name the endpoint a drop or a keyboard commit posts to with
				<span class="inline-code">data-gosx-reorder-action</span>
				, in the same
				<span class="inline-code">"METHOD /url"</span>
				grammar
				<span class="inline-code">data-gosx-action</span>
				uses.
			</p>
			{CodeBlock("gosx", `<ol data-gosx-reorder data-gosx-reorder-action="POST /api/board/reorder">
	    <li data-gosx-reorder-item="42">
	        <span data-gosx-reorder-handle aria-label="Reorder">⠿</span>
	        Player Name
	    </li>
	</ol>`)}
			<p>
				A drop posts the moved item's identity and its zero-based target index as
				<span class="inline-code">item_id</span>
				and
				<span class="inline-code">index</span>
				by default. Override the field names with
				<span class="inline-code">data-gosx-reorder-item-field</span>
				and
				<span class="inline-code">data-gosx-reorder-index-field</span>
				on the container.
			</p>
			<p>
				The runtime owns the full interaction, the reason a sortable list is a framework primitive and not a downstream widget:
			</p>
			<ul>
				<li>
					Pointer Events with
					<span class="inline-code">setPointerCapture</span>
					— one path for mouse and touch. The handle gets
					<span class="inline-code">touch-action: none</span>
					so a touch drag is not raced by the browser's own scroll gesture; every other pixel of the page keeps native scroll.
				</li>
				<li>
					The reorder is optimistic: the DOM updates on drop, before the action response returns. A failed action reverts the list to its pre-drag order and surfaces the failure through the same
					<span class="inline-code">data-gosx-form-state</span>
					/
					<span class="inline-code">data-gosx-pending</span>
					attributes and live-region announcement managed forms already use.
				</li>
				<li>
					Periodic revalidation (see
					<span class="inline-code">data-gosx-revalidate-interval</span>
					above) pauses for the whole gesture — pointerdown or grab through drop, cancel, or
					<span class="inline-code">pointercancel</span>
					— and resumes the moment it ends. Only the framework can coordinate this against its own revalidation loop.
				</li>
				<li>
					A second grab anywhere in the same container while a drop's action submission is still in flight is refused outright. The list never changes and no second request is issued.
				</li>
				<li>
					Escape and
					<span class="inline-code">pointercancel</span>
					cancel a pointer drag cleanly: the list returns to its pre-drag order and nothing is submitted.
				</li>
			</ul>
			<p>
				Keyboard reorder replaces a downstream app's arrow buttons on the same contract, not a decorative fallback:
			</p>
			<ul>
				<li>
					Space or Enter on the handle grabs the item.
				</li>
				<li>
					Arrow Up and Arrow Down move it one position while grabbed.
				</li>
				<li>
					Space drops it and posts the managed action.
				</li>
				<li>
					Escape cancels and restores the original order.
				</li>
			</ul>
			<p>
				Each step announces to an
				<span class="inline-code">aria-live</span>
				region: a grab announces the item and its position ("Grabbed Player Name. Position 1 of 12. Use arrow keys to move, space to drop, escape to cancel."), each arrow move announces the new position ("Moved to position 4 of 12."), and a drop confirms it.
			</p>
			<p>
				Style state with these class hooks — the runtime writes no inline style except
				<span class="inline-code">transform</span>
				on the lifted element while it tracks the pointer:
			</p>
			<ul>
				<li>
					<span class="inline-code">gosx-reorder--dragging</span>
					on the container while a gesture, pointer or keyboard, is active.
				</li>
				<li>
					<span class="inline-code">gosx-reorder-item--lifted</span>
					on the item being pointer-dragged. Give it
					<span class="inline-code">position: absolute</span>
					with no
					<span class="inline-code">top</span>
					or
					<span class="inline-code">left</span>
					— an absolutely positioned box with neither keeps its static, in-flow position, so it starts exactly where the runtime's
					<span class="inline-code">translateY</span>
					expects.
				</li>
				<li>
					<span class="inline-code">gosx-reorder-item--placeholder</span>
					on the clone that marks the item's live target slot during a pointer drag.
				</li>
				<li>
					<span class="inline-code">gosx-reorder-item--grabbed</span>
					on the item during a keyboard grab.
				</li>
			</ul>
			<p>
				<span class="inline-code">data-gosx-reorder</span>
				requires the navigation runtime (
				<span class="inline-code">app.EnableNavigation()</span>
				or
				<span class="inline-code">server.NavigationScript()</span>
				above) — periodic revalidation, the pause it pauses, and the managed-form error surface it reverts through all live there.
			</p>
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
