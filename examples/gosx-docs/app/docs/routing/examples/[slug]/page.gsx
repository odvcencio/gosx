package docs

func Page() Node {
	return <section class="docs-live-example">
		<p class="eyebrow">Dynamic route parameter</p>
		<h2>
			You opened:
			{params.slug}
		</h2>
		<p>
			The file route reads this value from its
			<code>[slug]</code>
			path segment.
		</p>
		<a href="/docs/routing" data-gosx-link="true">Back to routing</a>
		<a
			href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/docs/routing/examples/%5Bslug%5D/page.gsx"
			rel="noopener"
		>View the parameter route source</a>
	</section>
}
