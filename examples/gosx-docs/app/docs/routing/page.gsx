package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Routing</span>
			<p class="lede">
				File pages, redirects, rewrites, and navigation all live in the same mental model now.
			</p>
		</div>
		<h1>
			Routes can come from code or from the directory tree. Both are first-class now.
		</h1>
		<p>
			File-based routing lives in
			<span class="inline-code">route.Router</span>
			. The conventions are intentionally obvious: a directory becomes a URL segment,
			<span class="inline-code">page.gsx</span>
			becomes the page in that segment, and
			<span class="inline-code">[slug]</span>
			becomes a path parameter.
		</p>
		{DocsCodeBlock("go", `router := route.NewRouter()
		router.AddDir("./app", route.FileRoutesOptions{})`)}
		{DocsCodeBlock("json", `{
		  "cache": {
		    "public": true,
		    "maxAge": "45s",
		    "staleWhileRevalidate": "5m"
		  },
		  "cacheTags": ["docs-pages"]
		}`)}
		{DocsCodeBlock("go", `func init() {
		  route.MustRegisterFileModuleHere(route.FileModuleOptions{
		    Load:     ...,
		    Metadata: ...,
		    Actions:  ...,
		  })
		}`)}
		{DocsCodeBlock("go", `func init() {
		  route.MustRegisterDirModuleHere(route.DirModuleOptions{
		    Middleware: []route.Middleware{...},
		    Configure: func(ctx *route.RouteContext, page route.FilePage) error {
		      ctx.Header().Set("X-Docs-Section", "true")
		      return nil
		    },
		  })
		}`)}
		<section class="note-grid">
			<div class="note">
				<strong>Static pages</strong>
				<p>
					<span class="inline-code">app/about/page.gsx</span>
					maps to
					<span class="inline-code">/about</span>
					.
				</p>
			</div>
			<div class="note">
				<strong>Dynamic pages</strong>
				<p>
					<span class="inline-code">app/blog/[slug]/page.html</span>
					maps to
					<span class="inline-code">{`/blog/{slug}`}</span>
					.
				</p>
			</div>
			<div class="note">
				<strong>404 page</strong>
				<p>
					<span class="inline-code">app/not-found.gsx</span>
					becomes the router-level not-found page automatically.
				</p>
			</div>
			<div class="note">
				<strong>500 page</strong>
				<p>
					<span class="inline-code">app/error.gsx</span>
					becomes the router-level error fallback automatically.
				</p>
			</div>
			<div class="note">
				<strong>Server hooks</strong>
				<p>
					A sibling
					<span class="inline-code">page.server.go</span>
					file can attach loader, metadata, and action behavior without hard-coding the source path string.
				</p>
			</div>
			<div class="note">
				<strong>Directory config</strong>
				<p>
					A
					<span class="inline-code">route.config.json</span>
					file can set inherited cache, headers, and prerender defaults for an entire subtree.
				</p>
			</div>
			<div class="note">
				<strong>Scoped middleware</strong>
				<p>
					A nearby Go file can register
					<span class="inline-code">route.DirModuleOptions</span>
					when a subtree needs middleware or request setup that JSON cannot express.
				</p>
			</div>
		</section>
		<section class="callout">
			<strong>Redirects and rewrites</strong>
			<p>
				<span class="inline-code">server.App</span>
				now has declarative redirect and rewrite rules with path-value interpolation. That means aliases, moved docs, and canonical URL cleanup no longer require hand-written handlers.
			</p>
		</section>
		<div class="hero-actions">
			<a href="/docs/runtime" data-gosx-link class="cta-link primary">See the runtime path</a>
			<a href="/docs/forms" data-gosx-link class="cta-link">See routed form actions</a>
			<a href="/docs/getting-started" data-gosx-link class="cta-link">Back to getting started</a>
		</div>
	</article>
}
