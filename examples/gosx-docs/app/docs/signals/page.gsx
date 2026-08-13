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
			<span class="eyebrow">Reactive values</span>
			<p class="lede">
				The Go signal package provides concurrent reactive values for Go code. Island source uses a related, compiler-recognized subset that becomes browser VM instructions.
			</p>
		</div>
		<h2 id="go-signals">Go signals</h2>
		<CodeBlock lang="go" source={data.basicSample} />
		<p>
			<span class="inline-code">signal.New</span>
			returns
			<span class="inline-code">*signal.Signal[T]</span>
			.
			<span class="inline-code">Get</span>
			reads and participates in dependency tracking;
			<span class="inline-code">Set</span>
			and
			<span class="inline-code">Update</span>
			write;
			<span class="inline-code">Subscribe</span>
			returns an unsubscribe function.
			<span class="inline-code">Revision</span>
			is a non-tracking atomic change counter for polling.
		</p>
		<h2 id="equality">Equality and notifications</h2>
		<p>
			Comparable values receive a default equality check, so setting the same value does not notify. Slices, maps, and other non-comparable values notify on every
			<span class="inline-code">Set</span>
			unless you provide a comparator.
		</p>
		<CodeBlock lang="go" source={data.equalSample} />
		<p>
			Signal callbacks run without the signal mutex held. A callback may safely read the signal that notified it or other reactive values.
		</p>
		<h2 id="derived">Derived values</h2>
		<CodeBlock lang="go" source={data.deriveSample} />
		<p>
			<span class="inline-code">signal.Derive</span>
			tracks signal reads automatically. A computed value stays lazy after dependency changes while nobody subscribes; the next
			<span class="inline-code">Get</span>
			refreshes it. Call
			<span class="inline-code">Stop</span>
			when its lifetime ends.
		</p>
		<h2 id="effects">Effects</h2>
		<CodeBlock lang="go" source={data.watchSample} />
		<p>
			<span class="inline-code">signal.Watch</span>
			runs immediately, records dependencies read during the callback, and reruns when they change. It returns an
			<span class="inline-code">*Effect</span>
			; stop it with
			<span class="inline-code">Dispose</span>
			.
		</p>
		<h2 id="batching">Batching</h2>
		<CodeBlock lang="go" source={data.batchSample} />
		<p>
			<span class="inline-code">signal.Batch</span>
			defers subscriber notifications until the outermost batch returns, including during panic unwinding. It preserves queued callbacks; it does not promise to deduplicate repeated subscriptions or make every computed function run exactly once.
		</p>
		<section class="callout">
			<strong>One goroutine owns a batch</strong>
			<p>
				Do not write signals from another goroutine while a batch is open. Dependency tracking is goroutine-scoped; TinyGo's single-threaded runtime uses one shared scope.
			</p>
		</section>
		<h2 id="island-signals">Island signals</h2>
		<CodeBlock lang="gosx" source={data.islandSample} />
		<p>
			Inside a
			<span class="inline-code">//gosx:island</span>
			component, the compiler recognizes signal declarations, derived values, and handlers and lowers supported operations into the island VM. This is not arbitrary execution of the Go
			<span class="inline-code">signal</span>
			package in the browser.
		</p>
		<p>
			<span class="inline-code">signal.NewShared</span>
			is an island-source construct. Its normalized name begins with
			<span class="inline-code">$</span>
			and instances in the same document runtime can use that shared store entry. It is not a method on the server Go
			<span class="inline-code">signal</span>
			package.
		</p>
	</article>
}
