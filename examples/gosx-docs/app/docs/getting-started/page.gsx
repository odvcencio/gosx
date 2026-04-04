package docs

func Page() Node {
	return <div>
		<section id="overview" class="docs-section-block">
			<h2>Overview</h2>
			<p>
				GoSX is a Go-native web platform. You write Go, and the framework takes care of server rendering, routing, forms, auth, and real-time sync. There is no JavaScript build step and no second language. One
				<span class="inline-code">go build</span>
				produces a deployable binary with everything included.
			</p>
		</section>
		<section id="install" class="docs-section-block">
			<h2>Install</h2>
			<p>
				Install the GoSX CLI with a single
				<span class="inline-code">go install</span>
				command.
			</p>
			{CodeBlock("bash", "go install github.com/odvcencio/gosx/cmd/gosx@latest")}
			<p>
				Verify the installation by running
				<span class="inline-code">gosx --version</span>
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
			{CodeBlock("go", "// app/page.server.go\npackage app\n\nimport \"github.com/odvcencio/gosx/route\"\n\nfunc init() {\n\troute.MustRegisterFileModuleHere(route.FileModuleOptions{\n\t\tLoad: func(ctx *route.RouteContext, page route.FilePage) (any, error) {\n\t\t\treturn map[string]any{\n\t\t\t\t\"greeting\": \"Hello from the server\",\n\t\t\t}, nil\n\t\t},\n\t})\n}")}
			{CodeBlock("gsx", "// app/page.gsx\npackage app\n\nfunc Page() Node {\n\treturn <div>\n\t\t<h1>{data.greeting}</h1>\n\t</div>\n}")}
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
				Template changes reload the affected page without a full compile. Go source changes trigger a fast incremental rebuild. The browser tab reconnects automatically when the server is ready.
			</p>
		</section>
		<section id="next-steps" class="docs-section-block">
			<h2>Next Steps</h2>
			<p>
				Now that the dev server is running, explore what GoSX can do.
			</p>
			<ul>
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
