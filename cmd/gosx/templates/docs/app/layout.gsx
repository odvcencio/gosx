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

func DocsNavLink(props any) Node {
	return <>
		<If when={props.Active}>
			<a href={props.Href} data-gosx-link class="nav-link active">{props.Label}</a>
		</If>
		<If when={props.Active == false}>
			<a href={props.Href} data-gosx-link class="nav-link">{props.Label}</a>
		</If>
	</>
}

func DocsNavigation() Node {
	return <>
		<div class="nav-group">
			<span class="nav-group-title">Guide</span>
			<div class="nav-group-links">
				<DocsNavLink href="/" label="Overview" active={request.path == "/"}></DocsNavLink>
				<DocsNavLink href="/docs/getting-started" label="Getting Started" active={request.path == "/docs/getting-started"}></DocsNavLink>
				<DocsNavLink href="/docs/routing" label="Routing" active={request.path == "/docs/routing"}></DocsNavLink>
				<DocsNavLink href="/docs/forms" label="Forms" active={request.path == "/docs/forms"}></DocsNavLink>
				<DocsNavLink href="/docs/auth" label="Auth" active={request.path == "/docs/auth"}></DocsNavLink>
			</div>
		</div>
		<div class="nav-group">
			<span class="nav-group-title">Runtime</span>
			<div class="nav-group-links">
				<DocsNavLink href="/docs/runtime" label="Runtime" active={request.path == "/docs/runtime"}></DocsNavLink>
				<DocsNavLink href="/docs/images" label="Images" active={request.path == "/docs/images"}></DocsNavLink>
			</div>
		</div>
		<div class="nav-group">
			<span class="nav-group-title">Labs</span>
			<div class="nav-group-links">
				<DocsNavLink href="/labs/stream" label="Streaming" active={request.path == "/labs/stream"}></DocsNavLink>
				<DocsNavLink href="/labs/secret" label="Secret" active={request.path == "/labs/secret"}></DocsNavLink>
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
