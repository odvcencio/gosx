package docs

func Page() Node {
	return <main class="docs-live-example">
		<p class="eyebrow">Dynamic route parameter</p>
		<h2>You opened: {params.slug}</h2>
		<p>The file route reads this value from its <code>[slug]</code> path segment.</p>
		<a href="/docs/routing" data-gosx-link="true">Back to routing</a>
	</main>
}
