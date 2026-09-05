package app

func Page() Node {
	return <main class="shell">
		<span class="eyebrow">500</span>
		<h1>Something broke</h1>
		<p>The app hit an unexpected error while rendering the current page.</p>
		<div class="actions">
			<a href="/" data-gosx-link class="button primary">Back home</a>
		</div>
	</main>
}
