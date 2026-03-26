package docs

func Layout() Node {
	return <section class="docs-section">
		<div class="docs-section-head">
			<span class="eyebrow">Docs Path</span>
			<div class="docs-section-copy">
				<h2>Reference pages live inside a nested filesystem layout.</h2>
				<p class="lede">
					The <span class="inline-code">/docs</span> subtree now has its own layout and scoped not-found boundary on top of the site shell.
				</p>
			</div>
		</div>
		<Slot />
	</section>
}
