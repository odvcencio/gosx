package docs

func Layout() Node {
	return <div class="docs-shell">
		<header class="mobile-bar">
			<span class="mobile-kicker">Dogfood Docs</span>
			<a href="/" data-gosx-link class="brand">GoSX</a>
		</header>
		<aside class="sidebar">
			<div class="sidebar-frame">
				<div class="brand-lockup">
					<span class="eyebrow">Dogfood Docs</span>
					<a href="/" data-gosx-link class="brand">GoSX</a>
					<p class="brand-copy">
						The documentation site is rendered through file-routed .gsx pages and the built-in navigation runtime.
					</p>
				</div>
				<nav class="doc-nav">
					<a href="/" data-gosx-link class="nav-link">Overview</a>
					<a href="/docs/getting-started" data-gosx-link class="nav-link">Getting Started</a>
					<a href="/docs/routing" data-gosx-link class="nav-link">Routing</a>
					<a href="/docs/forms" data-gosx-link class="nav-link">Forms</a>
					<a href="/docs/auth" data-gosx-link class="nav-link">Auth</a>
					<a href="/docs/runtime" data-gosx-link class="nav-link">Runtime</a>
					<a href="/docs/images" data-gosx-link class="nav-link">Images</a>
					<a href="/labs/stream" data-gosx-link class="nav-link">Streaming</a>
					<a href="/labs/secret" data-gosx-link class="nav-link">Secret</a>
				</nav>
				<div class="sidebar-foot">
					<span class="foot-label">Shortcuts</span>
					<a href="/docs" class="chip">Docs redirect</a>
					<a href="/runtime" class="chip">Runtime rewrite</a>
					<a href="/api/meta" class="chip">Docs API</a>
				</div>
			</div>
		</aside>
		<main class="main">
			<Slot />
			<footer class="page-footer">
				GoSX docs dogfood file-based routing, page.server.go modules, sessions, CSRF, auth, image optimization, deferred streaming, client-side navigation, redirects, rewrites, public assets, and API routes.
			</footer>
		</main>
	</div>
}
