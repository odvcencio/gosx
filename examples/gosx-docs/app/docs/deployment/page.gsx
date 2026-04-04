package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Operations</span>
			<p class="lede">
				One
				<span class="inline-code">go build</span>
				produces a self-contained binary. Static export, ISR, and edge bundles are configuration choices, not architecture rewrites.
			</p>
		</div>
		<h1 id="build-modes">Build Modes</h1>
		<p>
			GoSX supports three production build modes. All three share the same source tree; the mode is selected at build time via the
			<span class="inline-code">gosx build</span>
			CLI flag.
		</p>
		<section class="feature-grid">
			<div class="card">
				<strong>SSR binary</strong>
				<p>
					Default mode. A single Go binary serves all routes with server-side rendering, ISR caching, and WebSocket hubs.
				</p>
			</div>
			<div class="card">
				<strong>Static export</strong>
				<p>
					All routes are pre-rendered to HTML files. Suitable for CDN deployment with no server process.
				</p>
			</div>
			<div class="card">
				<strong>Edge bundle</strong>
				<p>
					Routes compile to a lightweight WASM bundle for execution at the edge. Islands and 3D engines are excluded from the edge target.
				</p>
			</div>
		</section>
		<CodeBlock lang="bash" source={data.sampleBuildModes} />
		<h2 id="static-export">Static Export</h2>
		<p>
			<span class="inline-code">gosx export</span>
			crawls every route registered in the application, calls each
			<span class="inline-code">Load</span>
			function with a synthetic request context, renders the full HTML document, and writes the result to disk. The output directory mirrors the route tree.
		</p>
		<CodeBlock lang="bash" source={data.sampleExport} />
		<p>
			Assets under
			<span class="inline-code">public/</span>
			are copied verbatim. CSS is collected per-route and written to
			<span class="inline-code">_gosx/css/</span>
			with content-addressed filenames. The export step runs at Go speed — the entire gosx-docs site exports in under two seconds on a laptop.
		</p>
		<h2 id="server-deployment">Server Deployment</h2>
		<p>
			The production binary is statically linked and ships with all templates, assets, and the WASM runtime embedded. No external files are required at runtime. The binary listens on the port specified by
			<span class="inline-code">PORT</span>
			(default 8080) and responds to
			<span class="inline-code">SIGTERM</span>
			with a graceful drain.
		</p>
		<CodeBlock lang="bash" source={data.sampleServerBuild} />
		<p>
			All pages cached under ISR are stored in memory by default. Persistent ISR across restarts requires an external cache backend bound at startup. See the
			<a href="/docs/isr" class="inline-link">ISR reference</a>
			for the cache adapter interface.
		</p>
		<CodeBlock lang="go" source={data.sampleServerMain} />
		<h2 id="isr">ISR — Incremental Static Regeneration</h2>
		<p>
			ISR serves a cached pre-rendered page while revalidating in the background. The first request after a cache miss pays the render cost; every subsequent request is served from cache until the TTL expires. Stale-while-revalidate semantics mean there is no cold-start penalty on cache expiry.
		</p>
		<CodeBlock lang="go" source={data.sampleISR} />
		<p>
			Dynamic routes can opt out of ISR on a per-request basis by calling
			<span class="inline-code">ctx.NoCache()</span>
			inside
			<span class="inline-code">Load</span>
			. This is useful for authenticated pages where the response varies per user.
		</p>
		<section class="callout">
			<strong>ISR and islands</strong>
			<p>
				ISR caches the server-rendered HTML shell. Island state is initialised from the serialised signal values embedded in that shell. If the shell is stale, islands boot from stale initial values and update when signals change — which is usually correct for display data.
			</p>
		</section>
		<h2 id="edge-bundles">Edge Bundles</h2>
		<p>
			The edge target compiles route handlers and templates to a WASM module suitable for execution in Cloudflare Workers, Deno Deploy, or any runtime that supports the
			<span class="inline-code">wasi_snapshot_preview1</span>
			ABI. Islands and the 3D engine are excluded; edge routes must be pure SSR.
		</p>
		<CodeBlock lang="bash" source={data.sampleEdge} />
		<p>
			The manifest maps URL patterns to exported WASM function names so the edge adapter can route requests without parsing the WASM module. All static assets are expected to be served from a CDN; the edge handler returns only HTML and API responses.
		</p>
		<h2 id="docker">Docker</h2>
		<p>
			The recommended pattern is a two-stage Dockerfile: a builder stage that compiles the binary and a minimal runtime stage that ships it. Because the binary is statically linked and embeds all assets, the runtime image can be as small as
			<span class="inline-code">scratch</span>
			or
			<span class="inline-code">gcr.io/distroless/static</span>
			.
		</p>
		<CodeBlock lang="dockerfile" source={data.sampleDockerfile} />
		<p>
			The resulting image is typically 15–20 MB. No Node.js, no asset pipeline, no runtime dependencies beyond the OS libc provided by distroless. Push to any OCI-compatible registry and deploy with
			<span class="inline-code">kubectl</span>
			, Fly, or Railway.
		</p>
		<CodeBlock lang="bash" source={data.sampleDockerDeploy} />
		<section class="callout">
			<strong>No --build-context needed</strong>
			<p>
				gotreesitter is a released Go module, not a local C extension. The Dockerfile does not need
				<span class="inline-code">--build-context</span>
				or any CGo toolchain. A plain
				<span class="inline-code">docker build</span>
				is sufficient.
			</p>
		</section>
	</article>
}
