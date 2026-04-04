package docs

func NotFound() Node {
	return <section class="docs-section light">
		<div class="docs-grid">
			<div class="docs-content">
				<div class="docs-404">
					<h1>Page not found</h1>
					<p>
						This documentation page doesn't exist. Try one of these:
					</p>
					<ul>
						<li>
							<a href="/docs/getting-started" data-gosx-link="true">Getting Started</a>
						</li>
						<li>
							<a href="/docs/routing" data-gosx-link="true">Routing</a>
						</li>
						<li>
							<a href="/docs/forms" data-gosx-link="true">Forms</a>
						</li>
					</ul>
				</div>
			</div>
		</div>
	</section>
}
