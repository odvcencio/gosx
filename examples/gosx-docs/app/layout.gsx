package docs

func Layout() Node {
	return <div class="docs-shell">
		<a href="#docs-main" class="skip-link">Skip to content</a>
		<header class="mobile-bar">
			<div class="mobile-bar-head">
				<div class="mobile-branding">
					<span class="mobile-kicker">GoSX Application</span>
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
							<span class="eyebrow">GoSX Application</span>
							<a href="/" data-gosx-link class="brand">GoSX</a>
							<p class="brand-copy">
								Docs, demos, and production-ready flows in one GoSX app.
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
					<span class="eyebrow">GoSX Application</span>
					<a href="/" data-gosx-link class="brand">GoSX</a>
					<p class="brand-copy">
						Docs, demos, and production-ready flows in one GoSX app.
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
				GoSX lets the site, the docs, the editor, and the runtime route ship from one Go app.
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
			<span class="nav-group-title">Start</span>
			<div class="nav-group-links">
				<DocsNavLink href="/" label="Overview" active={request.path == "/"}></DocsNavLink>
				<DocsNavLink href="/docs/getting-started" label="Getting Started" active={request.path == "/docs/getting-started"}></DocsNavLink>
			</div>
		</div>
		<div class="nav-group">
			<span class="nav-group-title">Demos</span>
			<div class="nav-group-links">
				<DocsNavLink href="/demos/cms" label="CMS Demo" active={request.path == "/demos/cms"}></DocsNavLink>
				<DocsNavLink href="/demos/scene3d" label="Geometry Zoo" active={request.path == "/demos/scene3d"}></DocsNavLink>
			</div>
		</div>
		<div class="nav-group">
			<span class="nav-group-title">Docs</span>
			<div class="nav-group-links">
				<DocsNavLink href="/docs/routing" label="Routing" active={request.path == "/docs/routing"}></DocsNavLink>
				<DocsNavLink href="/docs/forms" label="Forms" active={request.path == "/docs/forms"}></DocsNavLink>
				<DocsNavLink href="/docs/auth" label="Auth" active={request.path == "/docs/auth"}></DocsNavLink>
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
			<a href="/demos/cms" data-gosx-link class="chip">CMS demo</a>
			<a href="/demos/scene3d" data-gosx-link class="chip">Geometry zoo</a>
		</div>
	</div>
}
