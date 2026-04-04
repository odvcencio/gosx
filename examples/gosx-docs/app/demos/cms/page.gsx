package docs

func Page() Node {
	return <div class="cms-demo">
		<header class="cms-header">
			<div class="cms-header__brand">
				<span class="cms-header__logo">GoSX CMS</span>
				<span class="cms-header__badge">Block Editor</span>
			</div>
			<div class="cms-header__actions">
				<form class="cms-publish-form" method="post" action={actionPath("publish")}>
					<input type="hidden" name="csrf_token" value={csrf.token} />
					<button type="submit" class="cms-btn cms-btn--primary">Publish</button>
				</form>
			</div>
		</header>

		<div class="cms-workspace">
			<aside class="cms-palette" aria-label="Block palette">
				<h2 class="cms-panel-title">Blocks</h2>
				<div class="cms-palette__list">
					<div class="cms-block-card" draggable="true" aria-label="Hero block">
						<span class="cms-block-card__icon" aria-hidden="true">H</span>
						<div class="cms-block-card__meta">
							<strong>Hero</strong>
							<span>Title + Subtitle</span>
						</div>
					</div>
					<div class="cms-block-card" draggable="true" aria-label="Feature block">
						<span class="cms-block-card__icon" aria-hidden="true">F</span>
						<div class="cms-block-card__meta">
							<strong>Feature</strong>
							<span>Title + Body</span>
						</div>
					</div>
					<div class="cms-block-card" draggable="true" aria-label="Quote block">
						<span class="cms-block-card__icon" aria-hidden="true">Q</span>
						<div class="cms-block-card__meta">
							<strong>Quote</strong>
							<span>Text + Author</span>
						</div>
					</div>
				</div>
			</aside>

			<main class="cms-editor" aria-label="Block editor">
				<h2 class="cms-panel-title">Editor</h2>
				<div class="cms-block-list">
					<Each of={data.blocks} as="block">
						<div class={"cms-editor-block cms-editor-block--" + block.type} role="group" aria-label={block.type + " block"}>
							<div class="cms-editor-block__header">
								<span class="cms-editor-block__type">{block.type}</span>
								<button class="cms-editor-block__remove" type="button" aria-label="Remove block">&#215;</button>
							</div>
							<div class="cms-editor-block__fields">
								<If cond={block.type == "hero"}>
									<label class="cms-field">
										<span>Title</span>
										<input class="cms-input" type="text" value={block.title} placeholder="Page title" />
									</label>
									<label class="cms-field">
										<span>Subtitle</span>
										<input class="cms-input" type="text" value={block.subtitle} placeholder="Supporting subtitle" />
									</label>
								</If>
								<If cond={block.type == "feature"}>
									<label class="cms-field">
										<span>Title</span>
										<input class="cms-input" type="text" value={block.title} placeholder="Feature title" />
									</label>
									<label class="cms-field">
										<span>Body</span>
										<textarea class="cms-input cms-input--textarea" placeholder="Feature description">{block.body}</textarea>
									</label>
								</If>
								<If cond={block.type == "quote"}>
									<label class="cms-field">
										<span>Quote</span>
										<textarea class="cms-input cms-input--textarea" placeholder="The quote text">{block.text}</textarea>
									</label>
									<label class="cms-field">
										<span>Author</span>
										<input class="cms-input" type="text" value={block.author} placeholder="Author name" />
									</label>
								</If>
							</div>
						</div>
					</Each>
				</div>
			</main>

			<aside class="cms-preview" aria-label="Live preview">
				<h2 class="cms-panel-title">Preview</h2>
				<div class="cms-preview__canvas">
					<Each of={data.blocks} as="block">
						<div class={"cms-preview-block cms-preview-block--" + block.type}>
							<If cond={block.type == "hero"}>
								<div class="cms-preview-hero">
									<h1>{block.title}</h1>
									<p>{block.subtitle}</p>
								</div>
							</If>
							<If cond={block.type == "feature"}>
								<div class="cms-preview-feature">
									<h3>{block.title}</h3>
									<p>{block.body}</p>
								</div>
							</If>
							<If cond={block.type == "quote"}>
								<figure class="cms-preview-quote">
									<blockquote>{block.text}</blockquote>
									<figcaption>— {block.author}</figcaption>
								</figure>
							</If>
						</div>
					</Each>
				</div>
			</aside>
		</div>

		<footer class="cms-statusbar">
			<span class="cms-statusbar__info">{len(data.blocks)} blocks</span>
			<span class="cms-statusbar__hint">Drag blocks from the palette to reorder</span>
		</footer>
	</div>
}
