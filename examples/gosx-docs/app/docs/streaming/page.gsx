package docs

func Page() Node {
	return <div>
		<section id="deferred-regions">
			<h2>Deferred Regions</h2>
			<p>
				A deferred region is a portion of the page that renders after the initial
				HTML shell is flushed to the browser.
				<span class="inline-code">ctx.Defer()</span>
				marks a node as deferred: the server emits a fallback immediately, continues
				flushing the rest of the page, and then streams the resolved content into the
				live DOM as the resolver function completes.
			</p>
			{CodeBlock("go", `func Load(ctx *route.RouteContext, page route.FilePage) (any, error) {
    deferred := ctx.Defer(
        gosx.El("p", gosx.Text("Loading...")),   // fallback
        func() (gosx.Node, error) {              // resolver
            data, err := fetchSlowData()
            if err != nil {
                return nil, err
            }
            return renderData(data), nil
        },
    )
    return map[string]any{"region": deferred}, nil
}`)}
			<p>
				The deferred node can be placed anywhere in the template. In the
				<span class="inline-code">.gsx</span>
				file it is injected like any other data value:
			</p>
			{CodeBlock("gosx", `func Page() Node {
    return <article>
        <h1>My page</h1>
        {data.region}
    </article>
}`)}
			<p>
				The live demo below uses a 200ms artificial delay to show the fallback
				and resolved states:
			</p>
			{streamDemo}
		</section>

		<section id="fallback-content">
			<h2>Fallback Content</h2>
			<p>
				The first argument to
				<span class="inline-code">ctx.Defer()</span>
				is the fallback node. It must be a complete, visible HTML subtree that is
				meaningful without the resolved content. Design fallbacks so the page is
				usable while deferred sections load.
			</p>
			<p>
				<span class="inline-code">ctx.DeferWithOptions()</span>
				adds a wrapping element with controllable attributes:
			</p>
			{CodeBlock("go", `deferred := ctx.DeferWithOptions(server.DeferredOptions{
    Class: "card skeleton",
    ID:    "slow-section",
},
    fallbackNode,
    resolverFunc,
)`)}
			<p>
				The options apply to the outer wrapper element that GoSX emits around both
				the fallback slot and the stream target. Setting a stable
				<span class="inline-code">ID</span>
				is useful when you want to target the wrapper from CSS or tests.
			</p>
			<section class="callout">
				<strong>Fallback must render without data</strong>
				<p>
					Fallback nodes are rendered synchronously as part of the initial flush.
					They cannot depend on the deferred resolver's result. Write them as
					skeleton states, spinners, or placeholder copy.
				</p>
			</section>
		</section>

		<section id="streaming-response">
			<h2>Streaming Response</h2>
			<p>
				When a page contains one or more deferred regions, GoSX switches the HTTP
				response to chunked transfer encoding. The server flushes:
			</p>
			<ol>
				<li>The document head and opening body markup.</li>
				<li>All synchronously-rendered HTML including fallback slots.</li>
				<li>A small inline script that the client uses to locate and replace each slot.</li>
				<li>As each resolver completes, a chunk containing the resolved HTML and a swap script.</li>
				<li>The closing body tag once all resolvers have finished.</li>
			</ol>
			<p>
				The browser receives and renders each chunk as it arrives.
				Time-to-first-byte is the server processing time for the synchronous shell,
				regardless of how long deferred resolvers take.
			</p>
			{CodeBlock("go", `// resolvers run concurrently — the slowest one determines total page time
slow := ctx.Defer(skeletonNode, func() (gosx.Node, error) {
    time.Sleep(800 * time.Millisecond) // slow database call
    return resolvedNode, nil
})
fast := ctx.Defer(skeletonNode, func() (gosx.Node, error) {
    time.Sleep(60 * time.Millisecond) // fast cache hit
    return resolvedNode, nil
})`)}
			<p>
				GoSX runs deferred resolvers concurrently. In the example above, the page
				takes roughly 800ms total, not 860ms, because the fast region resolves and
				streams while the slow region is still working.
			</p>
		</section>

		<section id="use-cases">
			<h2>Use Cases</h2>
			<p>
				Use streaming when part of the page is fast to render and another part
				depends on a slow or uncertain data source. Common patterns:
			</p>
			<ul>
				<li>
					<strong>Slow database queries.</strong>
					Render the page chrome and navigation synchronously while a heavy
					aggregation query runs in a deferred region.
				</li>
				<li>
					<strong>Personalized content after a static shell.</strong>
					Cache the static outer layout at the CDN edge. Defer the user-specific
					section so the personalized HTML streams from the origin.
				</li>
				<li>
					<strong>Third-party API calls.</strong>
					Embed inventory, pricing, or availability data that lives behind a slow
					external API without blocking the page chrome.
				</li>
				<li>
					<strong>Progressive disclosure.</strong>
					Stream supplementary content (related posts, recommendations) after the
					primary content is visible. Users can start reading while the page finishes.
				</li>
			</ul>
			<section class="callout">
				<strong>When not to use streaming</strong>
				<p>
					If the entire page depends on a single fast query, streaming adds
					complexity without benefit. Prefer a standard synchronous
					<span class="inline-code">Load</span>
					handler unless you have measured a real benefit from deferring specific
					regions.
				</p>
			</section>
			{CodeBlock("go", `// good candidate: product shell is fast, reviews are slow
func Load(ctx *route.RouteContext, page route.FilePage) (any, error) {
    product, err := db.GetProduct(id)    // fast, cached
    if err != nil {
        return nil, err
    }
    reviews := ctx.Defer(
        gosx.El("p", gosx.Text("Loading reviews...")),
        func() (gosx.Node, error) {
            return renderReviews(db.GetReviews(id)) // slow, not cached
        },
    )
    return map[string]any{
        "product": product,
        "reviews": reviews,
    }, nil
}`)}
		</section>
	</div>
}
