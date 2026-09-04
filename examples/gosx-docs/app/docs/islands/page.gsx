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
			<span class="eyebrow">Selective interaction</span>
			<p class="lede">
				An island is a named GSX component whose supported state, expressions, handlers, and markup are compiled into a program for GoSX's shared browser VM.
			</p>
		</div>
		<h2 id="island-model">Island model</h2>
		<p>
			Server rendering remains responsible for the page and its ordinary HTML. Islands opt specific regions into client behavior. They do not grant arbitrary DOM access or a general Go runtime; engines are the unrestricted client-computation tier.
		</p>
		<p>
			The build emits a manifest and browser-loadable program assets. At runtime, GoSX associates a mount with its component, props, and program reference, then evaluates the island tree and patches that mount.
		</p>
		<h2 id="authoring">Authoring an island</h2>
		<CodeBlock lang="gosx" source={data.counterSample} />
		<p>
			Place
			<span class="inline-code">//gosx:island</span>
			immediately before the component. The compiler recognizes local
			<span class="inline-code">signal.New</span>
			declarations and handler functions, then lowers the returned markup. Event properties such as
			<span class="inline-code">onClick</span>
			can reference a recognized handler.
		</p>
		<p>
			Use
			<span class="inline-code">gosx check</span>
			for isolated diagnostics and
			<span class="inline-code">gosx build</span>
			or
			<span class="inline-code">gosx dev</span>
			to produce and serve application assets.
		</p>
		<h2 id="composition">Pure-view composition</h2>
		<CodeBlock lang="gosx" source={data.compositionSample} />
		<p>
			A parent island may call a same-file strict pure-view component. The compiler inlines every invocation into the parent's program, including direct typed scalar props, caller-owned children, and named slots. Projected child markup can read the parent island's signals and bind its handlers. The browser still hydrates one root with one VM, and the encoded program has no component-call opcode.
		</p>
		<p>
			Pure-view means the callee owns no signals, computed values, handlers, or effects. Nested islands, engines, legacy and imported callees, handler-valued props, nested or slice props, spreads, recursion, and more than 32 composed component frames fail closed. Expanded programs retain the 65,535-node and expression limits.
		</p>
		<p>
			Caller children and named slots belong on an inner pure-view component, as in the sample. The root island itself cannot declare those holes because its
			<span class="inline-code">.gxi</span>
			asset is compiled before any individual server call site supplies content.
		</p>
		<h2 id="vm-subset">Expression VM subset</h2>
		<p>
			The island VM implements a constrained expression and statement language for signal reads and writes, literals, supported operators, property/index access, conditionals, and known handlers. The compiler rejects constructs it cannot encode.
		</p>
		<section class="callout">
			<strong>Compilation is the contract</strong>
			<p>
				A construct that is valid ordinary Go is not automatically valid in an island. Keep filesystem, network, reflection, package APIs, and unrestricted browser work on the server or in an engine.
			</p>
		</section>
		<h2 id="shared-signals">Shared signals</h2>
		<CodeBlock lang="gosx" source={data.sharedSample} />
		<p>
			<span class="inline-code">signal.NewShared</span>
			normalizes its key to a
			<span class="inline-code">$</span>
			signal name in the document runtime. Islands that use the same name can observe the same store entry. This sharing is browser-runtime state; it is not a Go
			<span class="inline-code">signal.Signal</span>
			returned by a route loader.
		</p>
		<p>
			Cross-frame synchronization is an additional relay layer with explicit peer and origin configuration. It is not implied by using a shared name.
		</p>
		<h2 id="program-assets">Program assets</h2>
		<CodeBlock lang="go" source={data.assetSample} />
		<p>
			File-based builds wire compiled islands automatically. Programmatic integrations can use
			<span class="inline-code">Island</span>
			or
			<span class="inline-code">IslandWithProgramAsset</span>
			on the page runtime. The latter records the exact asset reference, format, and optional hash that the browser should load.
		</p>
		<p>
			Program formats are runtime assets, not Go source embedded for browser evaluation. If an asset is missing or invalid, treat that as a build or deployment error rather than relying on undocumented recovery.
		</p>
		<h2 id="choosing">When to choose an island</h2>
		<ul>
			<li>
				Use a server component when HTML and request-time data are sufficient.
			</li>
			<li>
				Use an action for a mutation that can submit and return server state.
			</li>
			<li>
				Use an island for focused reactive DOM interaction in the supported VM subset.
			</li>
			<li>
				Use an engine for canvas, GPU, workers, media, or unrestricted client APIs.
			</li>
			<li>
				Use a hub when multiple clients need long-lived server coordination.
			</li>
		</ul>
	</article>
}
