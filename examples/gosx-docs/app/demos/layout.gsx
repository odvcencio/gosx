package docs

// Active-state strategy: inline script.
// The layout has no server-side access to the current request path — layouts
// do not have their own DataLoader and data is owned by the child page. Rather
// than plumbing a layout.server.go for a single attribute, a small inline
// script reads location.pathname on first paint and:
//   1. sets aria-current="page" on the matching dock item, and
//   2. sets data-demo-slug on .demos-shell for per-demo CSS accent overrides.
// This degrades gracefully at prerender (no active state), corrected on first
// JS tick. No external file needed.

func Layout() Node {
    return <div class="demos-shell">
	<header class="demos-topbar">
		<span class="demos-topbar__crumb">
			<a href="/" class="demos-topbar__home" data-gosx-link="true">GoSX</a>
			<span class="demos-topbar__sep" aria-hidden="true">/</span>
			<a href="/demos" class="demos-topbar__section" data-gosx-link="true">Demos</a>
		</span>
		<button
			class="demos-topbar__menu"
			type="button"
			aria-label="Toggle demos menu"
			aria-controls="demo-dock"
			aria-expanded="false"
		>
			<span class="demos-topbar__menu-bar" aria-hidden="true"></span>
			<span class="demos-topbar__menu-bar" aria-hidden="true"></span>
			<span class="demos-topbar__menu-bar" aria-hidden="true"></span>
		</button>
	</header>
	<div class="demos-body">
		<nav id="demo-dock" class="demo-dock" aria-label="Demos">
			<ul class="demo-dock__list" role="list">
				<Each of={Demos()} as="demo">
					<li class="demo-dock__item" role="listitem">
						<a
							href={"/demos/" + demo.Slug}
							class="demo-dock__link"
							data-gosx-link="true"
							data-demo={demo.Slug}
							data-demo-title={demo.Title}
							data-demo-lesson={demo.Lesson}
							data-demo-facets={demoValues(demo.Facets)}
							data-demo-source={demoSourceURL(demo.SourcePath)}
							data-demo-source-path={demo.SourcePath}
							data-demo-packages={demoValues(demo.Packages)}
							data-demo-render-mode={demo.RenderMode}
							data-demo-limitations={demo.Limitations}
						>
							<span class="demo-dock__dot" aria-hidden="true"></span>
							<span class="demo-dock__body">
								<span class="demo-dock__title">{demo.Title}</span>
								<span class="demo-dock__tag">{demo.Tag}</span>
							</span>
							<span class={"demo-dock__chip demo-dock__chip--" + demo.Status}>{demo.Status}</span>
						</a>
					</li>
				</Each>
			</ul>
		</nav>
		<div class="demo-viewport">
			<Slot />
			<footer class="demo-meta" role="contentinfo" aria-label="Demo metadata">
				<button
					type="button"
					class="demo-meta__pill"
					data-demo-details-open
					aria-controls="demo-details"
					aria-expanded="false"
				>How this is GoSX</button>
			</footer>
		</div>
	</div>
	<div class="demo-details-backdrop" data-demo-details-backdrop hidden></div>
	<aside
		id="demo-details"
		class="demo-details"
		role="dialog"
		aria-modal="true"
		aria-labelledby="demo-details-title"
		hidden
	>
		<header class="demo-details__header">
			<div>
				<p class="demo-details__eyebrow">How this is GoSX</p>
				<h2 id="demo-details-title" class="demo-details__title">Demo details</h2>
			</div>
			<button
				type="button"
				class="demo-details__close"
				data-demo-details-close
				aria-label="Close demo details"
			>×</button>
		</header>
		<p class="demo-details__lesson" data-demo-details-lesson></p>
		<dl class="demo-details__facts">
			<div>
				<dt>Built with</dt>
				<dd data-demo-details-facets></dd>
			</div>
			<div>
				<dt>Packages</dt>
				<dd data-demo-details-packages></dd>
			</div>
			<div>
				<dt>Rendering</dt>
				<dd data-demo-details-render-mode></dd>
			</div>
			<div>
				<dt>Honest limits</dt>
				<dd data-demo-details-limitations></dd>
			</div>
		</dl>
		<a class="demo-details__source" data-demo-details-source target="_blank" rel="noopener noreferrer">
			View GoSX source
			<span aria-hidden="true">↗</span>
		</a>
		<code class="demo-details__path" data-demo-details-source-path></code>
	</aside>
	<script src="/demos-dock.js" defer></script>
    </div>
}
