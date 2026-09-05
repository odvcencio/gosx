package app

func Page() Node {
	return <main class="shell">
		<span class="eyebrow">404</span>
		<h1>Page not found</h1>
		<p>
			Check your routes or drop the missing asset into
			<span class="inline-code">public/</span>.
		</p>
		<div class="actions">
			<a href="/" data-gosx-link class="button primary">Back home</a>
		</div>
	</main>
}
