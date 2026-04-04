package docs

func NotFound() Node {
	return <section class="error-page" aria-label="Page not found">
		<div class="error-page__inner">
			<h1 class="error-page__code chrome-text">404</h1>
			<p class="error-page__message">Page not found</p>
			<p class="error-page__hint">
				The page you're looking for doesn't exist or has been moved.
			</p>
			<div class="error-page__actions">
				<a href="/" data-gosx-link="true" class="error-page__link error-page__link--primary">Back to overview</a>
				<a href="/docs/getting-started" data-gosx-link="true" class="error-page__link">Getting started</a>
			</div>
		</div>
	</section>
}
