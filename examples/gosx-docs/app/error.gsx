package docs

func Error() Node {
	return <section class="error-page" aria-label="Error">
		<div class="error-page__inner">
			<div class="error-page__panel glass-panel">
				<h1 class="error-page__title">Something went wrong</h1>
				<p class="error-page__message">
					An unexpected error occurred. Please try again.
				</p>
				<div class="error-page__actions">
					<a href="/" data-gosx-link="true" class="error-page__link error-page__link--primary">Back to overview</a>
				</div>
			</div>
		</div>
	</section>
}
