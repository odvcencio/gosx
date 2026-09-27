package docs

import docsapp "m31labs.dev/gosx/examples/gosx-docs/app"

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
		<section id="overview" class="docs-section-block">
			<h2>Overview</h2>
			<p>
				GoSX is a Go-native web platform for server rendering, routing, forms, auth, interactive islands, realtime hubs, and managed graphics. GSX adds HTML-shaped markup to Go packages; the browser runtime is generated and managed by the framework, so an application does not need a JavaScript app toolchain.
				<span class="inline-code">gosx build --prod .</span>
				stages the server, file-route inputs, public content, hashed browser assets, and deployment metadata together in
				<span class="inline-code">dist/</span>
				.
			</p>
		</section>
		<section id="install" class="docs-section-block">
			<h2>Install</h2>
			<p>
				Install the GoSX CLI with a single
				<span class="inline-code">go install</span>
				command.
			</p>
			{CodeBlock("bash", docsapp.DocSample("getting-started/code-001.bash.sample"))}
			<p>
				Verify the installation by running
				<span class="inline-code">gosx version</span>
				.
			</p>
		</section>
		<section id="create-a-project" class="docs-section-block">
			<h2>Create a Project</h2>
			<p>
				The
				<span class="inline-code">gosx init</span>
				command scaffolds a new project with a runnable app, metadata, 404 and 500 pages, public assets, and the navigation runtime already wired up.
			</p>
			{CodeBlock("bash", docsapp.DocSample("getting-started/code-002.bash.sample"))}
			<p>
				Open
				<span class="inline-code">http://localhost:8080</span>
				to see the running application.
			</p>
		</section>
		<section id="project-structure" class="docs-section-block">
			<h2>Project Structure</h2>
			<p>
				A freshly scaffolded project looks like this. Pages live in
				<span class="inline-code">app/</span>
				, and each page is a pair of files: a
				<span class="inline-code">.gsx</span>
				template and an optional
				<span class="inline-code">page.server.go</span>
				for server-side data loading and actions.
			</p>
			{CodeBlock("text", docsapp.DocSample("getting-started/code-003.text.sample"))}
			<p>
				The
				<span class="inline-code">page.gsx</span>
				file is a Go-flavoured HTML template. The
				<span class="inline-code">page.server.go</span>
				sibling registers a server module that supplies data to the template through the
				<span class="inline-code">data</span>
				binding. Loader data is not available to a strict component yet. A route that reads
				<span class="inline-code">data</span>
				keeps the older
				<span class="inline-code">func Page() Node</span>
				form below; see "Component Syntax" further down.
			</p>
			{CodeBlock("go", docsapp.DocSample("getting-started/code-004.go.sample"))}
			{CodeBlock("gsx", docsapp.DocSample("getting-started/code-005.gsx.sample"))}
		</section>
		<section id="authoring-styles" class="docs-section-block">
			<h2>Component Syntax</h2>
			<p>
				Declare every component with the strict, typed form:
				<span class="inline-code">component Name(props: Type)</span>
				. The project-aware CLI checks
				<span class="inline-code">props</span>
				as an ordinary Go type.
			</p>
			{CodeBlock("gsx", docsapp.DocSample("getting-started/code-006.gsx.sample"))}
			<p>
				A strict server component allows one top-level GSX return and a narrow, renderer-safe expression set. A call uses an exact or unambiguous lower-camel prop name. It passes every field the callee renders explicitly, even a zero value.
			</p>
			<p>
				The older
				<span class="inline-code">func Name(...) Node</span>
				form is deprecated for ordinary components; GoSX removes its untyped variant before v1.0.
				<span class="inline-code">gosx check</span>
				already warns on it. It remains necessary today only for loader-bound routes (as above), islands, and engines. See
				<a href="/docs/components" data-gosx-link="true">Components</a>
				for the exact boundary and the migration note.
			</p>
		</section>
		<section id="dev-server" class="docs-section-block">
			<h2>Dev Server</h2>
			<p>
				Use
				<span class="inline-code">gosx dev</span>
				to start the development server with hot reload. The server watches your
				<span class="inline-code">.gsx</span>
				and
				<span class="inline-code">.go</span>
				files and recompiles the app on change.
			</p>
			{CodeBlock("bash", docsapp.DocSample("getting-started/code-007.bash.sample"))}
			<p>
				The command watches project source, rebuilds when needed, and refreshes connected browser tabs after a successful change. Compiler diagnostics remain in the terminal when a change is invalid.
			</p>
		</section>
		<section id="next-steps" class="docs-section-block">
			<h2>Next Steps</h2>
			<p>
				Now that the dev server is running, explore what GoSX can do.
			</p>
			<ul>
				<li>
					<a href="/docs/components" data-gosx-link="true">Components</a>
					— Strict component syntax, props, renderer boundaries, and the legacy migration note.
				</li>
				<li>
					<a href="/docs/routing" data-gosx-link="true">Routing</a>
					— File-based routing, dynamic params, and nested layouts.
				</li>
				<li>
					<a href="/docs/forms" data-gosx-link="true">Forms</a>
					— Server-side form handling with validation and CSRF protection.
				</li>
			</ul>
		</section>
	</div>
}
