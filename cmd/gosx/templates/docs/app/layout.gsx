package docs

func Layout() Node {
	return <div class="docs-shell">
		<a href="#docs-main" class="skip-link">Skip to content</a>
		<header class="mobile-bar">
			<div class="mobile-bar-head">
				<div class="mobile-branding">
					<span class="mobile-kicker">GoSX Docs</span>
					<a href="/" data-gosx-link class="brand">GoSX</a>
				</div>
				<details class="route-drawer">
					<summary class="route-drawer-summary">
						<span class="route-drawer-backdrop" aria-hidden="true"></span>
						<span class="route-drawer-toggle">
							<span class="route-drawer-toggle-kicker">Routes</span>
							<span class="route-drawer-toggle-copy route-drawer-toggle-copy-open">Browse sections</span>
							<span class="route-drawer-toggle-copy route-drawer-toggle-copy-close">Close</span>
						</span>
					</summary>
					<div class="route-drawer-panel">
						<div class="brand-lockup route-drawer-lockup">
							<span class="eyebrow">GoSX Docs</span>
							<a href="/" data-gosx-link class="brand">GoSX</a>
							<p class="brand-copy">
								Go-native web framework docs for routing, forms, auth, runtime, and the path from local app to production.
							</p>
						</div>
						<nav class="doc-nav">
							<DocsNavigation></DocsNavigation>
						</nav>
						<DocsShortcuts></DocsShortcuts>
					</div>
				</details>
			</div>
		</header>
		<aside class="sidebar">
			<div class="sidebar-frame">
				<div class="brand-lockup">
					<span class="eyebrow">GoSX Docs</span>
					<a href="/" data-gosx-link class="brand">GoSX</a>
					<p class="brand-copy">
						Go-native web framework docs for routing, forms, auth, runtime, and the path from local app to production.
					</p>
				</div>
				<nav class="doc-nav">
					<DocsNavigation></DocsNavigation>
				</nav>
				<DocsShortcuts></DocsShortcuts>
			</div>
		</aside>
		<main class="main" id="docs-main">
			<Slot />
			<footer class="page-footer">
				GoSX docs focus on the product: routing, server workflows, runtime behavior, and how to ship the framework in real apps.
			</footer>
		</main>
	</div>
}

func DocsNavigation() Node {
	return <>
		<div class="nav-group">
			<span class="nav-group-title">Guide</span>
			<div class="nav-group-links">
				<a href="/" data-gosx-link class="nav-link" aria-current={request.path == "/"}>Overview</a>
				<a
					href="/docs/getting-started"
					data-gosx-link
					class="nav-link"
					aria-current={request.path == "/docs/getting-started"}
				>Getting Started</a>
				<a
					href="/docs/routing"
					data-gosx-link
					class="nav-link"
					aria-current={request.path == "/docs/routing"}
				>Routing</a>
				<a href="/docs/forms" data-gosx-link class="nav-link" aria-current={request.path == "/docs/forms"}>Forms</a>
				<a href="/docs/auth" data-gosx-link class="nav-link" aria-current={request.path == "/docs/auth"}>Auth</a>
			</div>
		</div>
		<div class="nav-group">
			<span class="nav-group-title">Runtime</span>
			<div class="nav-group-links">
				<a
					href="/docs/runtime"
					data-gosx-link
					class="nav-link"
					aria-current={request.path == "/docs/runtime"}
				>Runtime</a>
				<a
					href="/docs/images"
					data-gosx-link
					class="nav-link"
					aria-current={request.path == "/docs/images"}
				>Images</a>
			</div>
		</div>
		<div class="nav-group">
			<span class="nav-group-title">Labs</span>
			<div class="nav-group-links">
				<a
					href="/labs/stream"
					data-gosx-link
					class="nav-link"
					aria-current={request.path == "/labs/stream"}
				>Streaming</a>
				<a
					href="/labs/secret"
					data-gosx-link
					class="nav-link"
					aria-current={request.path == "/labs/secret"}
				>Secret</a>
			</div>
		</div>
	</>
}

func DocsShortcuts() Node {
	return <div class="sidebar-foot">
		<span class="foot-label">Start here</span>
		<div class="shortcut-grid">
			<a href="/docs/getting-started" data-gosx-link class="chip">Quickstart</a>
			<a href="/docs/forms" data-gosx-link class="chip">Forms guide</a>
			<a href="/docs/runtime" data-gosx-link class="chip">Runtime guide</a>
		</div>
	</div>
}
