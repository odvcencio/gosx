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
		<h2 id="isr">Incremental static regeneration</h2>
		<p>
			ISR serves pages represented in the production export manifest and refreshes stale entries in the background. Enable it on the server and give an exported route a public cache lifetime. Cache tags travel into the export metadata for explicit invalidation.
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
