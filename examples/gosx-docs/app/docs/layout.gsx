package docs

func Layout() Node {
	return <div>
		<a class="skip-link" href="#toc">Skip to table of contents</a>
		<section class={if data.mode == "light" { "docs-section light" } else { "docs-section" }}>
			<div class="docs-grid">
				<nav id="toc" class="toc-rail" role="navigation" aria-label="Page contents">
					<Each of={data.toc} as="entry">
						<a href={entry.href} class="toc-link">{entry.label}</a>
					</Each>
				</nav>
				<article class="docs-content prose">
					<header class="docs-header">
						<h1 class="docs-header__title">{data.title}</h1>
						<p class="docs-header__description">{data.description}</p>
						<div class="docs-header__tags">
							<Each of={data.tags} as="tag">
								<span class="docs-tag">{tag}</span>
							</Each>
						</div>
					</header>
					<Slot />
					<footer class="docs-footer">
						<If cond={data.prev != nil}>
							<a
								href={data.prev.href}
								data-gosx-link="true"
								class="docs-footer__link docs-footer__link--prev"
							>
								{data.prev.label}
							</a>
						</If>
						<If cond={data.next != nil}>
							<a
								href={data.next.href}
								data-gosx-link="true"
								class="docs-footer__link docs-footer__link--next"
							>
								{data.next.label}
							</a>
						</If>
					</footer>
				</article>
			</div>
		</section>
	</div>
}
