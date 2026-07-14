package docs

// GoSX's managed navigation owns aria-current. The generic declarative binding
// below projects the active link's metadata into the shell and details drawer;
// toggles and accessible disclosure behavior are runtime capabilities too.

func Layout() Node {
    return <div
		class="demos-shell"
		data-gosx-bind-source=".demo-dock__link[aria-current='page']"
		data-gosx-bind-attr="data-demo-slug:data-demo"
	>
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
			data-gosx-toggle-target=".demos-body"
			data-gosx-toggle-attribute="data-dock-open"
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
							data-gosx-toggle-close=".demos-body"
							data-gosx-toggle-attribute="data-dock-open"
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
					data-gosx-disclosure-target="#demo-details"
					aria-controls="demo-details"
					aria-expanded="false"
				>How this is GoSX</button>
			</footer>
		</div>
	</div>
	<div class="demo-details-backdrop" data-gosx-disclosure-backdrop="#demo-details" hidden></div>
	<aside
		id="demo-details"
		class="demo-details"
		data-gosx-disclosure
		data-gosx-bind-source=".demo-dock__link[aria-current='page']"
		role="dialog"
		aria-modal="true"
		aria-labelledby="demo-details-title"
		hidden
	>
		<header class="demo-details__header">
			<div>
				<p class="demo-details__eyebrow">How this is GoSX</p>
				<h2 id="demo-details-title" class="demo-details__title" data-gosx-bind-text="data-demo-title">Demo details</h2>
			</div>
			<button
				type="button"
				class="demo-details__close"
				data-gosx-disclosure-close="#demo-details"
				data-gosx-disclosure-initial-focus
				aria-label="Close demo details"
			>×</button>
		</header>
		<p class="demo-details__lesson" data-gosx-bind-text="data-demo-lesson">Choose a demo to inspect how it is built.</p>
		<dl class="demo-details__facts">
			<div>
				<dt>Built with</dt>
				<dd data-gosx-bind-text="data-demo-facets">—</dd>
			</div>
			<div>
				<dt>Packages</dt>
				<dd data-gosx-bind-text="data-demo-packages">—</dd>
			</div>
			<div>
				<dt>Rendering</dt>
				<dd data-gosx-bind-text="data-demo-render-mode">—</dd>
			</div>
			<div>
				<dt>Honest limits</dt>
				<dd data-gosx-bind-text="data-demo-limitations">—</dd>
			</div>
		</dl>
		<a class="demo-details__source" data-gosx-bind-attr="href:data-demo-source" target="_blank" rel="noopener noreferrer">
			View GoSX source
			<span aria-hidden="true">↗</span>
		</a>
		<code class="demo-details__path" data-gosx-bind-text="data-demo-source-path"></code>
	</aside>
    </div>
}
