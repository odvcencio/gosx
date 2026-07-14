package docs

func Page() Node {
	return <form class="cms-demo" id="cms-content-form" method="post" action={actionPath("publish")}>
		<input type="hidden" name="csrf_token" value={csrf.token} />
		<input type="hidden" name="block_count" id="cms-block-count" value={len(data.blocks)} />
		<script src="/cms-client.js" defer></script>
		<header class="cms-header">
			<div class="cms-header__brand">
				<span class="cms-header__logo">CMS Editor</span>
				<span class="cms-header__badge">Server Action</span>
			</div>
			<div class="cms-header__actions">
				<span class="cms-header__unsaved" id="cms-unsaved-badge" role="status" aria-live="polite" hidden>
					<span class="cms-header__status-dot cms-header__status-dot--unsaved" aria-hidden="true"></span>
					Unsaved changes
				</span>
				<If cond={data.status.count > 0}>
					<span class="cms-header__status cms-header__status--published" id="cms-status">
						<span class="cms-header__status-dot" id="cms-status-dot"></span>
						<span id="cms-status-text">
							{"Published " + data.status.count + " blocks · " + data.status.at}
						</span>
					</span>
				</If>
				<If cond={data.status.count == 0}>
					<span class="cms-header__status" id="cms-status">
						<span class="cms-header__status-dot" id="cms-status-dot"></span>
						<span id="cms-status-text">{data.status.at}</span>
					</span>
				</If>
				<button type="submit" class="cms-btn cms-btn--primary" id="cms-publish-btn">
					<span class="cms-btn__label" id="cms-publish-label">Publish changes</span>
				</button>
			</div>
		</header>
		<div class="cms-workspace">
			<aside class="cms-palette" aria-label="Block types">
				<h2 class="cms-panel-label">Blocks</h2>
				<p class="cms-palette__hint">Click a block to add it to the draft</p>
				<div class="cms-palette__list" id="cms-palette-list">
					<button
						type="button"
						class="cms-block-card cms-block-card--hero"
						data-block-kind="hero"
						aria-label="Add Hero block"
					>
						<span class="cms-block-card__icon" aria-hidden="true">H</span>
						<div class="cms-block-card__meta">
							<strong>Hero</strong>
							<span>Title + Subtitle</span>
						</div>
					</button>
					<button
						type="button"
						class="cms-block-card cms-block-card--feature"
						data-block-kind="feature"
						aria-label="Add Feature block"
					>
						<span class="cms-block-card__icon" aria-hidden="true">F</span>
						<div class="cms-block-card__meta">
							<strong>Feature</strong>
							<span>Title + Body</span>
						</div>
					</button>
					<button
						type="button"
						class="cms-block-card cms-block-card--quote"
						data-block-kind="quote"
						aria-label="Add Quote block"
					>
						<span class="cms-block-card__icon" aria-hidden="true">Q</span>
						<div class="cms-block-card__meta">
							<strong>Quote</strong>
							<span>Text + Author</span>
						</div>
					</button>
				</div>
			</aside>
			<main class="cms-editor" aria-label="Block editor">
				<h2 class="cms-panel-label">Draft editor</h2>
				<div class="cms-block-list" id="cms-block-list">
					<Each of={data.blocks} as="block" index="i">
						<article
							class={"cms-editor-block cms-editor-block--" + block.kind}
							aria-label={block.kind + " block"}
							data-block-index={i}
							data-block-kind={block.kind}
						>
							<div class="cms-editor-block__header">
								<span class="cms-editor-block__type">{block.kind}</span>
							</div>
							<div class="cms-editor-block__fields">
								<input type="hidden" name={"block_" + i + "_kind"} value={block.kind} />
								<If cond={block.kind == "hero"}>
									<label class="cms-field">
										<span>Title</span>
										<input
											class="cms-input"
											data-preview-field="title"
											type="text"
											name={"block_" + i + "_title"}
											value={block.title}
											placeholder="Page title"
											required
											maxlength="120"
										 />
									</label>
									<label class="cms-field">
										<span>Subtitle</span>
										<input
											class="cms-input"
											data-preview-field="subtitle"
											type="text"
											name={"block_" + i + "_subtitle"}
											value={block.subtitle}
											placeholder="Supporting subtitle"
											maxlength="240"
										 />
									</label>
								</If>
								<If cond={block.kind == "feature"}>
									<label class="cms-field">
										<span>Title</span>
										<input
											class="cms-input"
											data-preview-field="title"
											type="text"
											name={"block_" + i + "_title"}
											value={block.title}
											placeholder="Feature title"
											required
											maxlength="120"
										 />
									</label>
									<label class="cms-field">
										<span>Body</span>
										<textarea
											class="cms-input cms-input--textarea"
											data-preview-field="body"
											name={"block_" + i + "_body"}
											placeholder="Feature description"
											maxlength="1000"
										>{block.body}</textarea>
									</label>
								</If>
								<If cond={block.kind == "quote"}>
									<label class="cms-field">
										<span>Quote</span>
										<textarea
											class="cms-input cms-input--textarea"
											data-preview-field="text"
											name={"block_" + i + "_text"}
											placeholder="The quote text"
											required
											maxlength="500"
										>{block.text}</textarea>
									</label>
									<label class="cms-field">
										<span>Author</span>
										<input
											class="cms-input"
											data-preview-field="author"
											type="text"
											name={"block_" + i + "_author"}
											value={block.author}
											placeholder="Author name"
											maxlength="120"
										 />
									</label>
								</If>
							</div>
						</article>
					</Each>
				</div>
			</main>
			<aside class="cms-preview" aria-label="Live preview">
				<h2 class="cms-panel-label">Live preview</h2>
				<div class="cms-preview__canvas" id="cms-preview-canvas">
					<Each of={data.blocks} as="block" index="i">
						<div class={"cms-preview-block cms-preview-block--" + block.kind} data-block-index={i}>
							<If cond={block.kind == "hero"}>
								<div class="cms-preview-hero">
									<h1 data-preview-field="title">{block.title}</h1>
									<span class="cms-preview-hero__divider"></span>
									<p data-preview-field="subtitle">{block.subtitle}</p>
								</div>
							</If>
							<If cond={block.kind == "feature"}>
								<div class="cms-preview-feature">
									<h3 data-preview-field="title">{block.title}</h3>
									<p data-preview-field="body">{block.body}</p>
								</div>
							</If>
							<If cond={block.kind == "quote"}>
								<figure class="cms-preview-quote">
									<blockquote data-preview-field="text">{block.text}</blockquote>
									<hr />
									<figcaption>
										—
										<span data-preview-field="author">{block.author}</span>
									</figcaption>
								</figure>
							</If>
						</div>
					</Each>
				</div>
			</aside>
		</div>
		<footer class="cms-statusbar">
			<span class="cms-statusbar__info">
				<span id="cms-block-total">{len(data.blocks)}</span>
				blocks in document
			</span>
			<span class="cms-statusbar__hint" id="cms-publish-feedback" role="status" aria-live="polite"></span>
		</footer>
		<div class="cms-sr-only" aria-live="polite" id="cms-announcer"></div>
	</form>
}
