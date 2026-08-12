package docs

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
			{CodeBlock("bash", "go install m31labs.dev/gosx/cmd/gosx@latest")}
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
			{CodeBlock("bash", "gosx init my-app\ncd my-app\ngo run .")}
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
			{CodeBlock("text", "my-app/\n├── app/\n│   ├── layout.gsx          # Root layout shared by all pages\n│   ├── page.gsx            # Home page template\n│   ├── page.server.go      # Home page server module\n│   ├── error.gsx           # 500 error page\n│   └── not-found.gsx       # 404 page\n├── public/                 # Static assets served at /\n├── main.go                 # App entry point\n└── go.mod")}
			<p>
				The
				<span class="inline-code">page.gsx</span>
				file is a Go-flavoured HTML template. The
				<span class="inline-code">page.server.go</span>
				sibling registers a server module that supplies data to the template through the
				<span class="inline-code">data</span>
				binding.
			</p>
			{CodeBlock("go", "// app/page.server.go\npackage app\n\nimport (\n\t\"log\"\n\n\t\"m31labs.dev/gosx/route\"\n)\n\nfunc init() {\n\tif err := route.RegisterFileModuleHere(route.FileModuleOptions{\n\t\tLoad: func(ctx *route.RouteContext, page route.FilePage) (any, error) {\n\t\t\treturn map[string]any{\n\t\t\t\t\"greeting\": \"Hello from the server\",\n\t\t\t}, nil\n\t\t},\n\t}); err != nil {\n\t\tlog.Fatal(err)\n\t}\n}")}
			{CodeBlock("gsx", "// app/page.gsx\npackage app\n\nfunc Page() Node {\n\treturn <div>\n\t\t<h1>{data.greeting}</h1>\n\t</div>\n}")}
		</section>
		<section id="authoring-styles" class="docs-section-block">
			<h2>Choose an Authoring Style</h2>
			<p>
				GoSX supports strict typed components and the established Go-function form side by side. Keep route pages that read loader
				<span class="inline-code">data</span>
				in the legacy form shown above. Use the TSX-like strict form for small same-file components with explicit Go prop types.
			</p>
			{CodeBlock("gsx", "package app\n\ntype BadgeProps struct {\n\tLabel string\n\tCount int\n}\n\ncomponent Badge(props: BadgeProps) {\n\treturn <span className=\"badge\">\n\t\t{props.Label}: {props.Count}\n\t</span>\n}\n\ncomponent Page() {\n\treturn <main><Badge label=\"Inbox\" count={0} /></main>\n}")}
			<p>
				Strict server components deliberately allow one top-level GSX return and narrow renderer-safe expressions. Calls use exact or unambiguous lower-camel prop names and explicitly pass every field the callee renders, even zero values. Use the legacy form for local statements, structural control flow, helpers, renderer builtins, client directives, or dynamic route bindings. See
				<a href="/docs/components" data-gosx-link="true">Components</a>
				for the exact boundary.
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
			{CodeBlock("bash", "gosx dev")}
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
					— Strict typed and legacy authoring styles, props, and renderer boundaries.
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
