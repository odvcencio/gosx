package docs

func Layout() Node {
	return <div class="site-shell">
		<a class="skip-link" href="#main-content">Skip to content</a>
		<a class="skip-link" href="#pill-nav">Skip to navigation</a>

		<nav id="pill-nav" class="pill-nav" role="navigation" aria-label="Main navigation">
			<a href="/" class="pill-logo" data-gosx-link="true">GoSX</a>
			<button class="pill-toggle" aria-expanded="false" aria-controls="nav-overlay" aria-label="Toggle navigation menu">
				<span class="pill-toggle__bar"></span>
				<span class="pill-toggle__bar"></span>
				<span class="pill-toggle__bar"></span>
			</button>
		</nav>

		<div id="nav-overlay" class="nav-overlay" role="dialog" aria-modal="true" aria-label="Site navigation" hidden>
			<div class="nav-overlay__inner">
				<div class="nav-group">
					<span class="nav-group__label">Start</span>
					<a href="/" data-gosx-link="true" class="nav-link">Overview</a>
					<a href="/docs/getting-started" data-gosx-link="true" class="nav-link">Getting Started</a>
				</div>
				<div class="nav-group">
					<span class="nav-group__label">Reference</span>
					<a href="/docs/routing" data-gosx-link="true" class="nav-link">Routing</a>
					<a href="/docs/forms" data-gosx-link="true" class="nav-link">Forms</a>
					<a href="/docs/auth" data-gosx-link="true" class="nav-link">Auth</a>
					<a href="/docs/islands" data-gosx-link="true" class="nav-link">Islands</a>
					<a href="/docs/signals" data-gosx-link="true" class="nav-link">Signals</a>
					<a href="/docs/engines" data-gosx-link="true" class="nav-link">Engines</a>
					<a href="/docs/scene3d" data-gosx-link="true" class="nav-link">3D Engine</a>
					<a href="/docs/hubs" data-gosx-link="true" class="nav-link">Hubs & CRDT</a>
					<a href="/docs/runtime" data-gosx-link="true" class="nav-link">Runtime</a>
					<a href="/docs/images" data-gosx-link="true" class="nav-link">Images</a>
					<a href="/docs/text-layout" data-gosx-link="true" class="nav-link">Text Layout</a>
					<a href="/docs/motion" data-gosx-link="true" class="nav-link">Motion</a>
					<a href="/docs/streaming" data-gosx-link="true" class="nav-link">Streaming</a>
					<a href="/docs/compiler" data-gosx-link="true" class="nav-link">Compiler</a>
					<a href="/docs/deployment" data-gosx-link="true" class="nav-link">Deployment</a>
				</div>
				<div class="nav-group">
					<span class="nav-group__label">Demos</span>
					<a href="/demos/galaxy" data-gosx-link="true" class="nav-link">Galaxy</a>
					<a href="/demos/scene3d" data-gosx-link="true" class="nav-link">Geometry Zoo</a>
					<a href="/demos/cms" data-gosx-link="true" class="nav-link">CMS Editor</a>
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
					<span class="site-footer__tagline">Go-native web platform</span>
				</div>
				<div class="site-footer__links">
					<a href="https://github.com/odvcencio/gosx" class="site-footer__link" rel="noopener">GitHub</a>
				</div>
				<div class="site-footer__a11y">
					<p>GoSX is committed to accessibility. This site targets WCAG 2.2 AA compliance.</p>
				</div>
			</div>
		</footer>

		<script src="/reveal.js" defer></script>
	</div>
}
