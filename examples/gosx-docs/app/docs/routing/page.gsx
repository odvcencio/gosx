package docs

func Page() Node {
	return <div>
		<section id="file-routes" class="docs-section-block">
			<h2>File Routes</h2>
			<p>
				GoSX uses the <span class="inline-code">app/</span> directory as the source of truth for routes.
				Every <span class="inline-code">page.gsx</span> file becomes a route. The URL path mirrors the
				directory path under <span class="inline-code">app/</span>.
			</p>
			{CodeBlock("text", "app/\n├── page.gsx              → /\n├── about/\n│   └── page.gsx          → /about\n└── blog/\n    ├── page.gsx          → /blog\n    └── [slug]/\n        └── page.gsx      → /blog/:slug")}
			<p>
				Routes are registered at startup by calling <span class="inline-code">router.AddDir</span> from
				<span class="inline-code">main.go</span>. The router walks the directory tree and wires every
				<span class="inline-code">page.gsx</span> to its corresponding URL pattern.
			</p>
			{CodeBlock("go", "router := route.NewRouter()\nif err := router.AddDir(filepath.Join(root, \"app\"), route.FileRoutesOptions{}); err != nil {\n\tlog.Fatal(err)\n}")}
		</section>

		<section id="dynamic-params" class="docs-section-block">
			<h2>Dynamic Params</h2>
			<p>
				Wrap a directory name in square brackets to create a dynamic segment. The matched value is
				available in the server module as <span class="inline-code">ctx.Params</span> and in the template
				as the <span class="inline-code">params</span> binding.
			</p>
			{CodeBlock("text", "app/\n└── blog/\n    └── [slug]/\n        ├── page.gsx\n        └── page.server.go")}
			{CodeBlock("go", "// app/blog/[slug]/page.server.go\nfunc init() {\n\troute.MustRegisterFileModuleHere(route.FileModuleOptions{\n\t\tLoad: func(ctx *route.RouteContext, page route.FilePage) (any, error) {\n\t\t\tslug := ctx.Params[\"slug\"]\n\t\t\tpost, err := db.FindPost(slug)\n\t\t\tif err != nil {\n\t\t\t\treturn nil, err\n\t\t\t}\n\t\t\treturn map[string]any{\"post\": post}, nil\n\t\t},\n\t})\n}")}
			{CodeBlock("gsx", "<h1>{data.post.title}</h1>\n<p>{params.slug}</p>")}
			<p>
				Use <span class="inline-code">__catch-all</span> as the directory name to match any remaining
				path segments. The full unmatched path is available as <span class="inline-code">params[\"*\"]</span>.
			</p>
		</section>

		<section id="layouts" class="docs-section-block">
			<h2>Layouts</h2>
			<p>
				A <span class="inline-code">layout.gsx</span> file wraps every page in its directory and all
				subdirectories. The page content is injected via the <span class="inline-code">Slot</span> component.
				Layouts nest: a subdirectory layout wraps its pages inside the parent layout.
			</p>
			{CodeBlock("gsx", "// app/docs/layout.gsx\npackage docs\n\nfunc Layout() Node {\n\treturn <div class=\"docs-wrapper\">\n\t\t<nav class=\"docs-nav\">\n\t\t\t<Each of={data.toc} as=\"entry\">\n\t\t\t\t<a href={entry.href}>{entry.label}</a>\n\t\t\t</Each>\n\t\t</nav>\n\t\t<main>\n\t\t\t<Slot />\n\t\t</main>\n\t</div>\n}")}
			<p>
				The root layout lives at <span class="inline-code">app/layout.gsx</span> and is used as the
				document shell. The server sets the HTML document wrapper separately so the root layout focuses
				on the visual chrome.
			</p>
		</section>

		<section id="data-loading" class="docs-section-block">
			<h2>Data Loading</h2>
			<p>
				Each page can have a sibling <span class="inline-code">page.server.go</span> that registers a
				<span class="inline-code">Load</span> function. The function runs on the server before the page
				is rendered. Its return value is available in the template as <span class="inline-code">data</span>.
			</p>
			{CodeBlock("go", "func init() {\n\troute.MustRegisterFileModuleHere(route.FileModuleOptions{\n\t\tLoad: func(ctx *route.RouteContext, page route.FilePage) (any, error) {\n\t\t\titems, err := db.ListItems()\n\t\t\tif err != nil {\n\t\t\t\treturn nil, err\n\t\t\t}\n\t\t\treturn map[string]any{\n\t\t\t\t\"items\": items,\n\t\t\t\t\"count\": len(items),\n\t\t\t}, nil\n\t\t},\n\t})\n}")}
			<p>
				Return any Go value — a struct, a map, a slice. The template binds it as <span class="inline-code">data</span>
				and accesses fields with dot notation: <span class="inline-code">{"{data.count}"}</span>.
			</p>
		</section>

		<section id="redirects-and-rewrites" class="docs-section-block">
			<h2>Redirects &amp; Rewrites</h2>
			<p>
				Register redirects and rewrites directly on the <span class="inline-code">server.App</span>
				instance. Redirects send the browser to a new URL. Rewrites proxy the request to a different
				internal path without changing the browser URL.
			</p>
			{CodeBlock("go", "app.Redirect(\"GET /docs\", \"/docs/getting-started\", http.StatusTemporaryRedirect)\napp.Redirect(\"GET /old-path\", \"/new-path\", http.StatusMovedPermanently)")}
			<p>
				Use <span class="inline-code">route.Config</span> files in a directory to set page-level options
				such as caching policies and revalidation intervals without touching Go code.
			</p>
		</section>

		<section id="client-navigation" class="docs-section-block">
			<h2>Client Navigation</h2>
			<p>
				Add the <span class="inline-code">data-gosx-link</span> attribute to any anchor to enable
				client-side navigation. The runtime fetches the next page over a persistent connection,
				swaps the content, and updates the browser history — no full page reload.
			</p>
			{CodeBlock("gsx", "<a href=\"/docs/forms\" data-gosx-link=\"true\">Forms</a>")}
			<p>
				The navigation script is injected by calling <span class="inline-code">server.NavigationScript()</span>
				from the root layout function in <span class="inline-code">main.go</span>. Omit it to keep every
				navigation as a full server round-trip.
			</p>
		</section>
	</div>
}
