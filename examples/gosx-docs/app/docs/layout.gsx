package docs

func Layout() Node {
	return <div>
		<a class="skip-link" href="#docs-content">Skip to documentation</a>
		<section class={docsSectionClassName}>
			<div class="docs-grid">
				<aside class="docs-rail" aria-label="Documentation navigation">
					<nav
						class="docs-guide-navigation docs-guide-navigation--desktop"
						aria-label="All documentation guides"
					>
						<a href="/docs" data-gosx-link="true" class={docsIndexClassName} aria-current={docsIndexCurrent}>
							<span>Search &amp; guide index</span>
							<small>All documentation</small>
						</a>
						<Each of={docsNavigation} as="section">
							<section class="docs-guide-group" aria-label={section.title}>
								<h2>{section.title}</h2>
								<Each of={section.entries} as="entry">
									<a
										href={entry.href}
										data-gosx-link="true"
										class={entry.className}
										aria-current={entry.ariaCurrent}
									>{entry.title}</a>
								</Each>
							</section>
						</Each>
					</nav>
					<details class="docs-guide-disclosure">
						<summary>Guide index</summary>
						<nav
							class="docs-guide-navigation docs-guide-navigation--mobile"
							aria-label="All documentation guides"
						>
							<a
								href="/docs"
								data-gosx-link="true"
								class={docsIndexClassName}
								aria-current={docsIndexCurrent}
							>
								<span>Search &amp; guide index</span>
								<small>All documentation</small>
							</a>
							<Each of={docsNavigation} as="section">
								<section class="docs-guide-group" aria-label={section.title}>
									<h2>{section.title}</h2>
									<Each of={section.entries} as="entry">
										<a
											href={entry.href}
											data-gosx-link="true"
											class={entry.className}
											aria-current={entry.ariaCurrent}
										>{entry.title}</a>
									</Each>
								</section>
							</Each>
						</nav>
					</details>
					<nav id="toc" class="docs-page-toc" aria-label="On this page">
						<p>On this page</p>
						<Each of={data.toc} as="entry">
							<a href={entry.href} class="toc-link">{entry.label}</a>
						</Each>
					</nav>
				</aside>
				<article id="docs-content" class="docs-content prose" tabindex="-1">
					<header class="docs-header">
						<p class="docs-header__eyebrow kicker">GoSX Docs</p>
						<h1 class="docs-header__title">{data.title}</h1>
						<p class="docs-header__description">{data.description}</p>
						<div class="docs-header__tags">
							<Each of={data.tags} as="tag">
								<span class="docs-tag">{tag}</span>
							</Each>
						</div>
						<If cond={docsLiveDemo != nil}>
							<a class="docs-header__live" href={docsLiveDemo.href} data-gosx-link="true">
								<span class="docs-header__live-kicker">See it live</span>
								<span class="docs-header__live-title">{docsLiveDemo.title}</span>
								<span aria-hidden="true">↗</span>
							</a>
						</If>
					</header>
					<Slot />
					<footer class="docs-footer">
						<If cond={docsSourceURL != nil}>
							<a href={docsSourceURL} rel="noopener" class="docs-footer__source">
								<span>Page source</span>
								<code>{docsSourcePath}</code>
							</a>
						</If>
						<If cond={docsPrevious != nil}>
							<a
								href={docsPrevious.href}
								data-gosx-link="true"
								class="docs-footer__link docs-footer__link--prev"
							>
								{docsPrevious.label}
							</a>
						</If>
						<If cond={docsNext != nil}>
							<a href={docsNext.href} data-gosx-link="true" class="docs-footer__link docs-footer__link--next">
								{docsNext.label}
							</a>
						</If>
					</footer>
				</article>
			</div>
		</section>
	</div>
}
