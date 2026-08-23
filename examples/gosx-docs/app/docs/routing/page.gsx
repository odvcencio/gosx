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
			<span class="eyebrow">Filesystem to request handler</span>
			<p class="lede">
				GoSX discovers page and layout files, maps directory names to route patterns, and pairs each page with an optional request-aware server module.
			</p>
		</div>
		<h2 id="file-routes">File routes</h2>
		<CodeBlock lang="text" source={data.treeSample} />
		<p>
			<span class="inline-code">page.gsx</span>
			or
			<span class="inline-code">index.gsx</span>
			creates a route. Directories in parentheses are route groups and do not appear in the URL. Scoped
			<span class="inline-code">not-found.gsx</span>
			and
			<span class="inline-code">error.gsx</span>
			files handle failures below their directory.
		</p>
		<h2 id="params">Dynamic and catch-all parameters</h2>
		<p>
			Use
			<span class="inline-code">[slug]</span>
			or Go-package-friendly
			<span class="inline-code">_slug</span>
			for one segment. Use
			<span class="inline-code">[...path]</span>
			or
			<span class="inline-code">__path</span>
			for a catch-all. In both spellings, the parameter name is the declared name:
			<span class="inline-code">ctx.Param("path")</span>
			.
		</p>
		<h2 id="layouts">Layouts</h2>
		<p>
			A
			<span class="inline-code">layout.gsx</span>
			wraps pages below its directory. Nested layouts compose from root to leaf, and
			<span class="inline-code">&lt;Slot /&gt;</span>
			marks where the child layout or page renders.
		</p>
		<h2 id="modules">Server modules and loader data</h2>
		<CodeBlock lang="go" source={data.moduleSample} />
		<CodeBlock lang="gosx" source={data.pageSample} />
		<p>
			<span class="inline-code">RegisterFileModuleHere</span>
			infers the sibling page source from its caller. A module may provide
			<span class="inline-code">Load</span>
			,
			<span class="inline-code">Metadata</span>
			, managed-action installers on the shared route router ,
			<span class="inline-code">Bindings</span>
			, or a custom
			<span class="inline-code">Render</span>
			. Handle registration errors instead of using deprecated must-register helpers.
		</p>
		<p>
			Loader results appear as the dynamic
			<span class="inline-code">data</span>
			binding in legacy page components. Typed strict route-props binding is not part of v0.39.
		</p>
		<h2 id="configuration">Directory configuration</h2>
		<CodeBlock lang="json" source={data.configSample} />
		<p>
			<span class="inline-code">route.config.json</span>
			is directory-scoped and inherited. It supports cache policy, cache tags, response headers, and static-export
			<span class="inline-code">prerender</span>
			selection. More specific directories override inherited fields.
		</p>
		<p>
			Use
			<span class="inline-code">route.NotFound</span>
			from a loader for a route-level 404. Configure server redirects with
			<span class="inline-code">app.Redirect</span>
			. GoSX does not expose a file-route rewrite declaration in this release.
		</p>
		<h2 id="navigation">Managed navigation</h2>
		<p>
			Enable navigation on the server and include
			<span class="inline-code">server.NavigationScript()</span>
			in the document head. Links marked
			<span class="inline-code">data-gosx-link="true"</span>
			can fetch the next server-rendered document and swap managed content while preserving the browser history contract.
		</p>
		<p>
			The server remains authoritative on every navigation. External links, downloads, modified clicks, and links outside the managed contract continue through native browser navigation.
		</p>
	</article>
}
