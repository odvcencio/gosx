package docs

func Layout() Node {
	return <div class="site-shell">
		<a class="skip-link" href="#main-content">Skip to content</a>
		<a class="skip-link" href="#pill-nav">Skip to navigation</a>
		<nav id="pill-nav" class="pill-nav" role="navigation" aria-label="Main navigation">
			<a href="/" class="pill-logo" data-gosx-link="true" aria-label="GoSX home">GoSX</a>
			<div class="pill-links">
				<a href="/docs" data-gosx-link="true" class="pill-link">Docs</a>
				<a href="/demos" data-gosx-link="true" class="pill-link">Demos</a>
				<form class="nav-search" action="/docs" method="get" role="search">
					<label class="visually-hidden" for="site-search">Search documentation</label>
					<input id="site-search" name="q" type="search" placeholder="Search docs" autocomplete="off" />
					<button type="submit">Search</button>
				</form>
				<a href="https://github.com/odvcencio/gosx" rel="noopener" class="pill-link">GitHub</a>
				<a
					href="/api/site"
					class="pill-version"
					aria-label={"Running GoSX version " + site.frameworkVersion}
				>{site.frameworkVersion}</a>
			</div>
			<a
				class="pill-toggle"
				href="#nav-overlay"
				aria-controls="nav-overlay"
				aria-label="Open navigation menu"
			>
				<span class="pill-toggle__bar"></span>
				<span class="pill-toggle__bar"></span>
				<span class="pill-toggle__bar"></span>
			</a>
		</nav>
		<div id="nav-overlay" class="nav-overlay" role="dialog" aria-modal="true" aria-label="Site navigation">
			<div class="nav-overlay__inner">
				<a class="nav-overlay__close" href="#main-content" aria-label="Close navigation menu">Close</a>
				<form class="overlay-search" action="/docs" method="get" role="search">
					<label for="overlay-site-search">Search all guides</label>
					<div>
						<input
							id="overlay-site-search"
							name="q"
							type="search"
							placeholder="Components, routing, Scene3D…"
							autocomplete="off"
						 />
						<button type="submit">Search</button>
					</div>
				</form>
				<div class="nav-overlay__groups">
					<div class="nav-group">
						<span class="nav-group__label">Learn</span>
						<a href="/docs" data-gosx-link="true" class="nav-link">All documentation</a>
						<a href="/docs/getting-started" data-gosx-link="true" class="nav-link">Getting started</a>
						<a href="/docs/components" data-gosx-link="true" class="nav-link">Components</a>
						<a href="/docs/typed-live" data-gosx-link="true" class="nav-link">Typed component proof</a>
					</div>
					<div class="nav-group">
						<span class="nav-group__label">See it work</span>
						<a href="/demos" data-gosx-link="true" class="nav-link">All demos</a>
						<a href="/demos/playground" data-gosx-link="true" class="nav-link">Compiler playground</a>
						<a href="/demos/collab" data-gosx-link="true" class="nav-link">Realtime collaboration</a>
						<a href="/demos/beacon" data-gosx-link="true" class="nav-link">Scene3D world</a>
					</div>
					<div class="nav-group">
						<span class="nav-group__label">Project</span>
						<a href="https://github.com/odvcencio/gosx" rel="noopener" class="nav-link">Source on GitHub</a>
						<a href="https://github.com/odvcencio/gosx/releases/tag/v0.39.0" rel="noopener" class="nav-link">v0.39.0 release notes</a>
						<a href="/api/site" class="nav-link">Running build metadata</a>
					</div>
				</div>
			</div>
		</div>
		<main id="main-content">
			<Slot />
		</main>
		<footer class="site-footer" role="contentinfo">
			<div class="site-footer__inner">
				<div class="site-footer__brand">
					<span class="site-footer__logo chrome-text">GoSX</span>
					<span class="site-footer__tagline">
						This documentation is a GoSX application.
					</span>
				</div>
				<div class="site-footer__links" aria-label="Project links">
					<a href="/docs" data-gosx-link="true" class="site-footer__link">Documentation</a>
					<a href="/demos" data-gosx-link="true" class="site-footer__link">Live demos</a>
					<a href="https://github.com/odvcencio/gosx" class="site-footer__link" rel="noopener">Source</a>
				</div>
				<div class="site-footer__build">
					<a href="/api/site" class="site-footer__version">
						Running GoSX
						{site.frameworkVersion}
						·
						{site.revision}
					</a>
					<p>
						Server-rendered routes, actions, islands, hubs, and managed GPU scenes are exercised by this site and its demos.
					</p>
				</div>
			</div>
		</footer>
	</div>
}
