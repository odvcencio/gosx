package docs

func Page() Node {
	return <article class="prose">
		<section class="docs-live-example" aria-label="Running deployment identity">
			<p class="eyebrow">Running deployment</p>
			<p>
				Framework:
				{data.buildInfo.frameworkVersion}
			</p>
			<p>
				Revision:
				{data.buildInfo.revision}
			</p>
			<p>
				Build time:
				{data.buildInfo.builtAt}
			</p>
			<a href="/api/site">Open the machine-readable build record</a>
			<a href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/site.go" rel="noopener">View the build record source</a>
		</section>
		<div class="page-topper">
			<span class="eyebrow">Operations</span>
			<p class="lede">
				A production build stages one deployable
				<span class="inline-code">dist/</span>
				bundle: the server binary when the project is runnable, file-route inputs, public content, hashed browser assets, prerendered pages, and platform metadata.
			</p>
		</div>
		<h2 id="build-output">Build the deployable bundle</h2>
		<p>
			<span class="inline-code">gosx build</span>
			accepts an application directory. Development is the default; select production explicitly for hashed assets, a production server build, static prerendering, and edge/platform output.
		</p>
		<CodeBlock lang="bash" source={data.sampleBuildModes} />
		<p>
			Outputs are additive. The offline and Windows packaging flags extend the same production bundle; edge and platform files are part of the normal runnable production output rather than a separate target mode.
		</p>
		<CodeBlock lang="text" source={data.sampleOutput} />
		<h2 id="static-export">Static export</h2>
		<p>
			<span class="inline-code">gosx export .</span>
			builds the runnable application, requests each eligible non-parameterized file route, and writes HTML below
			<span class="inline-code">dist/static</span>
			plus route metadata in
			<span class="inline-code">dist/export.json</span>
			. Dynamic parameter routes are not invented during export, and a route scope can opt out with
			<span class="inline-code">&#123; "prerender": false &#125;</span>
			in
			<span class="inline-code">route.config.json</span>
			.
		</p>
		<p>
			Pages with a
			<span class="inline-code">Load</span>
			hook or
			<span class="inline-code">Actions</span>
			stay dynamic by default. To export a public snapshot, set
			<span class="inline-code">&#123; "prerender": true &#125;</span>
			in that route's
			<span class="inline-code">route.config.json</span>
			. This setting also applies to child routes unless they override it. Private responses and responses that set cookies remain excluded.
		</p>
		<CodeBlock lang="bash" source={data.sampleExport} />
		<p>
			The exporter rewrites page and asset references for nested static paths and copies only runtime assets referenced by exported documents. Treat
			<span class="inline-code">dist/static</span>
			as the static-host root and retain
			<span class="inline-code">dist/export.json</span>
			when another GoSX deployment layer needs route metadata.
		</p>
		<h2 id="edge-output">Prerender edge worker</h2>
		<p>
			A production build of a runnable app writes
			<span class="inline-code">dist/edge/worker.js</span>
			and
			<span class="inline-code">dist/platform/deployment.json</span>
			. The worker serves exported routes and static assets through an
			<span class="inline-code">ASSETS</span>
			binding, then proxies misses, dynamic routes, and mutations to
			<span class="inline-code">GOSX_ORIGIN</span>
			(or
			<span class="inline-code">ORIGIN</span>
			).
		</p>
		<CodeBlock lang="bash" source={data.sampleEdge} />
		<section class="callout">
			<strong>Not an edge WASM server</strong>
			<p>
				The generated worker does not compile Go route handlers into a portable server-side WASM module. Keep an origin for anything absent from the prerendered route table.
			</p>
		</section>
		<h2 id="server-deployment">Server deployment</h2>
		<p>
			For a runnable
			<span class="inline-code">package main</span>
			, the build places the executable at
			<span class="inline-code">dist/server/app</span>
			and writes
			<span class="inline-code">dist/run.sh</span>
			. Deploy the whole bundle: file-routed apps can read staged
			<span class="inline-code">app/</span>
			,
			<span class="inline-code">content/</span>
			, and
			<span class="inline-code">public/</span>
			at runtime, while
			<span class="inline-code">build.json</span>
			maps hashed browser assets.
		</p>
		<CodeBlock lang="bash" source={data.sampleServerRun} />
		<h2 id="compression">Response compression</h2>
		<CodeBlock lang="go" source={data.sampleCompression} />
		<p>
			GoSX compresses HTML and other text responses by default. Existing apps need no code change. It prefers Brotli when the request allows it, falls back to gzip, and sends uncompressed bytes when neither is accepted. Call
			<span class="inline-code">app.DisableCompression()</span>
			before starting the server to opt out. Existing calls to
			<span class="inline-code">app.EnableGzip()</span>
			still work and add no extra compression while the default is enabled.
		</p>
		<p>
			A production build writes
			<span class="inline-code">.br</span>
			and
			<span class="inline-code">.gz</span>
			files beside compressible public files and exported HTML when they save bytes. The server and generated edge worker select these variants; missing server variants use dynamic compression. Responses smaller than 1 KiB and binary files stay uncompressed. Streams flush immediately; a flush before 1 KiB keeps that stream uncompressed. Encoding variants use
			<span class="inline-code">Vary: Accept-Encoding</span>
			so caches can keep them separate.
		</p>
		<h2 id="isr">Incremental static regeneration</h2>
		<p>
			ISR serves pages represented in the production export manifest and refreshes stale entries in the background. Enable it on the server and give an exported route a public cache lifetime. Cache tags travel into the export metadata for explicit invalidation.
		</p>
		<p>
			An exported page with
			<span class="inline-code">RevalidateSeconds=0</span>
			keeps its build-time data until a rebuild or explicit invalidation. The build warns when a prerendered page has a
			<span class="inline-code">Load</span>
			hook and no revalidation window. For changing public data, opt in to prerendering and set a public cache lifetime. Keep request-specific data dynamic. Requests with cookies or authorization bypass the snapshot and render the origin, including at the exported trailing-slash URL. Dynamic file pages also accept the same trailing-slash URL after they leave the export manifest.
		</p>
		<CodeBlock lang="json" source={data.sampleISRConfig} />
		<CodeBlock lang="go" source={data.sampleISRApp} />
		<p>
			The default ISR store is process-local. A multi-instance deployment can install a shared
			<span class="inline-code">server.ISRStore</span>
			with
			<span class="inline-code">app.SetISRStore</span>
			; the Redis package provides
			<span class="inline-code">redis.NewISRStore</span>
			. This artifact store is distinct from the revalidation-version store.
		</p>
		<h2 id="offline-windows">Offline and Windows bundles</h2>
		<p>
			<span class="inline-code">--offline</span>
			stages
			<span class="inline-code">dist/offline</span>
			with its own versioned manifest and the available static, runtime, app, and public inputs. The desktop host can open that directory through its
			<span class="inline-code">app://gosx</span>
			bundle transport.
		</p>
		<CodeBlock lang="bash" source={data.sampleOffline} />
		<p>
			<span class="inline-code">--msix</span>
			packages a runnable Windows build. It requires a Windows target or host and the Windows packaging tools; signing and AppInstaller generation are additional explicit options.
		</p>
		<h2 id="docker">Container example</h2>
		<p>
			Build in a toolchain image, then copy
			<span class="inline-code">dist/</span>
			as a unit into an image with a shell for
			<span class="inline-code">run.sh</span>
			. Choose a smaller base only after proving your own binary and system-library requirements.
		</p>
		<CodeBlock lang="dockerfile" source={data.sampleDockerfile} />
	</article>
}
