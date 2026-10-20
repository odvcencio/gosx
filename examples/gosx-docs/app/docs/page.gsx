package docs

func Page() Node {
	return <div class="docs-index">
		<section id="search" class="docs-search" aria-labelledby="docs-search-title">
			<div class="docs-search__heading">
				<p class="docs-search__eyebrow">Server-rendered search</p>
				<h2 id="docs-search-title">Find the surface you need</h2>
				<p>
					This form submits an ordinary GET request. Results arrive in HTML, remain linkable, and work before the GoSX browser runtime starts.
				</p>
			</div>
			<form class="docs-search__form" method="get" action="/docs" role="search">
				<label for="docs-query">Search all guides</label>
				<div class="docs-search__controls">
					<input
						id="docs-query"
						name="q"
						type="search"
						value={data.query}
						placeholder="Try: strict props, CSRF, WebGPU…"
						autocomplete="off"
					 />
					<button type="submit">Search docs</button>
				</div>
				<p class="docs-search__hint">
					Searches titles, descriptions, keywords, routes, and the source file behind every guide.
				</p>
			</form>
			<If cond={data.hasQuery}>
				<div class="docs-search__result-heading">
					<p class="docs-search__summary" role="status">{data.resultSummary}</p>
					<a href="/docs" data-gosx-link="true" class="docs-search__clear">Clear search</a>
				</div>
			</If>
			<If cond={data.hasResults}>
				<ol class="docs-results" aria-label="Documentation search results">
					<Each of={data.results} as="result">
						<li class="docs-result">
							<div class="docs-result__meta">
								<span>{result.section}</span>
								<code>{result.href}</code>
							</div>
							<h3>
								<a href={result.href} data-gosx-link="true">{result.title}</a>
							</h3>
							<p>{result.description}</p>
							<details class="docs-result__provenance">
								<summary>Search evidence</summary>
								<dl>
									<div>
										<dt>Source</dt>
										<dd>
											<code>{result.source}</code>
										</dd>
									</div>
									<div>
										<dt>Keywords</dt>
										<dd>{result.keywords}</dd>
									</div>
								</dl>
							</details>
						</li>
					</Each>
				</ol>
			</If>
			<If cond={data.noResults}>
				<div class="docs-search__empty">
					<p>
						No catalog entry matched every search term.
					</p>
					<p>
						Try a capability name such as
						<a href="/docs?q=islands">islands</a>
						,
						<a href="/docs?q=actions">actions</a>
						, or
						<a href="/docs?q=webgpu">WebGPU</a>
						.
					</p>
				</div>
			</If>
		</section>
		<section id="browse" class="docs-directory" aria-labelledby="docs-directory-title">
			<div class="docs-directory__intro">
				<p class="docs-directory__eyebrow">Complete guide</p>
				<h2 id="docs-directory-title">Browse by execution surface</h2>
				<p>
					Each guide links to the exact source that renders it. The index, navigation rail, search, and sitemap all read the same Go catalog.
				</p>
			</div>
			<div class="docs-directory__sections">
				<Each of={data.directorySections} as="section">
					<section class="docs-directory__section" aria-labelledby={section.id}>
						<header>
							<h3 id={section.id}>{section.title}</h3>
							<p>{section.description}</p>
						</header>
						<ul>
							<Each of={section.entries} as="entry">
								<li>
									<a href={entry.href} data-gosx-link="true">
										<span class="docs-directory__entry-title">{entry.title}</span>
										<span class="docs-directory__entry-description">{entry.description}</span>
										<code>{entry.source}</code>
									</a>
								</li>
							</Each>
						</ul>
					</section>
				</Each>
			</div>
		</section>
	</div>
}
