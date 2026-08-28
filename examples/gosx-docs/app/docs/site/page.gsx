package docs

func Page() Node {
	return <article class="site-guide">
		<section id="request-path" class="docs-section-block">
			<h2>One request, explicit upgrades</h2>
			<p>
				This page begins as server-rendered HTML from a file route. The shared GoSX navigation layer upgrades internal links. Individual demos opt into actions, islands, hubs, or engines only when their behavior needs them.
			</p>
			<div class="site-guide__flow" aria-label="Documentation request flow">
				<span>HTTP request</span>
				<span>file route + loader</span>
				<span>server HTML</span>
				<span>optional managed runtime</span>
			</div>
		</section>
		<section id="proof-map" class="docs-section-block">
			<h2>Where each surface is exercised</h2>
			<div class="site-guide__proofs">
				<article>
					<span>Server</span>
					<h3>Every guide and catalog page</h3>
					<p>
						File routes, metadata, layouts, loaders, navigation, sessions, and ISR are part of the app serving this page.
					</p>
					<a href="/docs/typed-live" data-gosx-link="true">Inspect the strict typed route</a>
				</article>
				<article>
					<span>Action + Island</span>
					<h3>Forms and compiler playground</h3>
					<p>
						The forms guide posts a named, CSRF-protected server action. The playground compiles and hydrates its focused legacy island subset.
					</p>
					<a href="/demos/playground" data-gosx-link="true">Run the compiler playground</a>
				</article>
				<article>
					<span>Hub</span>
					<h3>Collaboration and simulation</h3>
					<p>
						Collab, Chinese Checkers, velocity field, and live physics mount real GoSX hubs. Their state is intentionally process-local, so the deployment remains one replica.
					</p>
					<a href="/demos/collab" data-gosx-link="true">Test two-tab convergence</a>
				</article>
				<article>
					<span>Engine</span>
					<h3>Scene3D and managed graphics</h3>
					<p>
						The homepage and graphics demos ship typed scenes. The browser reports the backend it actually selected instead of claiming WebGPU in advance.
					</p>
					<a href="/demos/beacon" data-gosx-link="true">Open the Scene3D world</a>
				</article>
			</div>
		</section>
		<section id="deployment" class="docs-section-block">
			<h2>What is live</h2>
			<p>
				The production build stages the server, route source, static content, hashed browser assets, manifests, and launch script into one
				<span class="inline-code">dist/</span>
				bundle. A minimal container runs that bundle as a non-root user. Kubernetes pins the image by digest and probes GoSX health and readiness endpoints.
			</p>
			<ul>
				<li>
					<a href="/api/site">
						Machine-readable build and runtime metadata
					</a>
				</li>
				<li>
					<a href="/healthz">Liveness endpoint</a>
				</li>
				<li>
					<a href="/readyz">Readiness endpoint</a>
				</li>
				<li>
					<a href="/sitemap.xml">Generated public route map</a>
				</li>
				<li>
					<a href="https://github.com/odvcencio/gosx/tree/main/deploy" rel="noopener">Deployment source</a>
				</li>
			</ul>
		</section>
		<section id="limits" class="docs-section-block">
			<h2>Deliberate limits</h2>
			<ul>
				<li>
					Realtime demo state is in memory and is not durable across a rollout.
				</li>
				<li>
					The playground accepts a focused, rate-limited source subset; it is not a remote general-purpose Go builder.
				</li>
				<li>
					GPU backend and performance depend on the visitor's browser, adapter, and driver.
				</li>
				<li>
					Docs auth uses process-local demo identity. It demonstrates framework flow, not a production account service.
				</li>
			</ul>
		</section>
	</article>
}
