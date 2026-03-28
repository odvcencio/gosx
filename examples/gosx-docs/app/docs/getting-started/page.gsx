package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Start Here</span>
			<p class="lede">
				The first-use path: init a project, route real pages, and adopt the server metadata, public asset, and API surface.
			</p>
		</div>
		<h1>
			Start with one command, then keep the mental model server-first.
		</h1>
		<p>
			The short path is now
			<span class="inline-code">gosx init my-app</span>
			. That scaffold gives you a runnable app with metadata, APIs, 404 and 500 pages, root-level public assets, and the navigation runtime already wired in.
		</p>
		{DocsCodeBlock("bash", `gosx init my-app
		cd my-app
		go run .`)}
		<section class="callout">
			<strong>Server modules</strong>
			<p>
				If you add sibling
				<span class="inline-code">page.server.go</span>
				files, import your
				<span class="inline-code">/app</span>
				package from
				<span class="inline-code">main.go</span>
				once so those init-time registrations execute.
			</p>
			{DocsCodeBlock("go", `import (
		  _ "your/module/app"
		)
		
		route.MustRegisterFileModuleHere(route.FileModuleOptions{ ... })`)}
		</section>
		<div class="feature-grid">
			<section class="card">
				<strong>Project entry</strong>
				<p>
					<span class="inline-code">main.go</span>
					still owns your app. GoSX removes wiring, not control.
				</p>
			</section>
			<section class="card">
				<strong>HTML pages</strong>
				<p>
					<span class="inline-code">{`route.Router.AddDir("./app", ...)`}</span>
					turns file-routed
					<span class="inline-code">.gsx</span>
					pages into server-rendered routes.
				</p>
			</section>
			<section class="card">
				<strong>JSON endpoints</strong>
				<p>
					<span class="inline-code">app.API(...)</span>
					colocates plain backend routes with pages when you need them.
				</p>
			</section>
			<section class="card">
				<strong>Assets</strong>
				<p>
					Anything in
					<span class="inline-code">public/</span>
					is available at the root URL path.
				</p>
			</section>
		</div>
		<section class="callout">
			<strong>Environment convention</strong>
			<p>
				GoSX now loads
				<span class="inline-code">.env</span>
				,
				<span class="inline-code">.env.local</span>
				, and mode-specific variants through
				<span class="inline-code">env.LoadDir</span>
				.
			</p>
		</section>
		<section class="callout">
			<strong>Default shape</strong>
			<p>
				The starter app now pushes page authoring into
				<span class="inline-code">app/</span>
				files first, leaving
				<span class="inline-code">main.go</span>
				mostly responsible for APIs, middleware, and server concerns.
			</p>
		</section>
		<div class="hero-actions">
			<a href="/docs/routing" data-gosx-link class="cta-link primary">Continue to routing</a>
			<a href="/docs/forms" data-gosx-link class="cta-link">See form handling</a>
			<a href="/" data-gosx-link class="cta-link">Back to overview</a>
		</div>
	</article>
}
