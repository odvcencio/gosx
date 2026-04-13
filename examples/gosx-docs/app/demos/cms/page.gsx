package docs

func Page() Node {
	return <div class="cms-demo">
		<header class="cms-header">
			<div class="cms-header__brand">
				<span class="cms-header__logo">CMS Editor</span>
				<span class="cms-header__badge">Block Editor</span>
			</div>
			<div class="cms-header__actions">
				<If cond={data.status.count > 0}>
					<span class={"cms-header__status cms-header__status--published"}>
						<span class="cms-header__status-dot"></span>
						{"Published " + fmt.Sprint(data.status.count) + " blocks · " + fmt.Sprint(data.status.at)}
					</span>
				</If>
				<If cond={data.status.count == 0}>
					<span class="cms-header__status">
						<span class="cms-header__status-dot"></span>
						{data.status.at}
					</span>
				</If>
				<form class="cms-publish-form" method="post" action={actionPath("publish")}>
					<input type="hidden" name="csrf_token" value={csrf.token} />
					<button type="submit" class="cms-btn cms-btn--primary">Publish</button>
				</form>
			</div>
		</header>
		<div class="cms-workspace">
			<aside class="cms-palette" aria-label="Block types">
				<h2 class="cms-panel-label">Blocks</h2>
				<div class="cms-palette__list">
					<div class="cms-block-card cms-block-card--hero">
						<span class="cms-block-card__icon" aria-hidden="true">H</span>
						<div class="cms-block-card__meta">
							<strong>Hero</strong>
							<span>Title + Subtitle</span>
						</div>
					</div>
					<div class="cms-block-card cms-block-card--feature">
						<span class="cms-block-card__icon" aria-hidden="true">F</span>
						<div class="cms-block-card__meta">
							<strong>Feature</strong>
							<span>Title + Body</span>
						</div>
					</div>
					<div class="cms-block-card cms-block-card--quote">
						<span class="cms-block-card__icon" aria-hidden="true">Q</span>
						<div class="cms-block-card__meta">
							<strong>Quote</strong>
							<span>Text + Author</span>
						</div>
					</div>
				</div>
			</aside>
			<main class="cms-editor" aria-label="Block editor">
				<h2 class="cms-panel-label">Editor</h2>
				<div class="cms-block-list">
					<Each of={data.blocks} as="block">
						<article
							class={"cms-editor-block cms-editor-block--" + block.kind}
							aria-label={block.kind + " block"}
						>
							<div class="cms-editor-block__header">
								<span class="cms-editor-block__type">{block.kind}</span>
							</div>
							<div class="cms-editor-block__fields">
								<If cond={block.kind == "hero"}>
									<label class="cms-field">
										<span>Title</span>
										<input class="cms-input" type="text" name="title" value={block.title} placeholder="Page title" />
									</label>
									<label class="cms-field">
										<span>Subtitle</span>
										<input class="cms-input" type="text" name="subtitle" value={block.subtitle} placeholder="Supporting subtitle" />
									</label>
								</If>
								<If cond={block.kind == "feature"}>
									<label class="cms-field">
										<span>Title</span>
										<input class="cms-input" type="text" name="title" value={block.title} placeholder="Feature title" />
									</label>
									<label class="cms-field">
										<span>Body</span>
										<textarea class="cms-input cms-input--textarea" name="body" placeholder="Feature description">{block.body}</textarea>
									</label>
								</If>
								<If cond={block.kind == "quote"}>
									<label class="cms-field">
										<span>Quote</span>
										<textarea class="cms-input cms-input--textarea" name="text" placeholder="The quote text">{block.text}</textarea>
									</label>
									<label class="cms-field">
										<span>Author</span>
										<input class="cms-input" type="text" name="author" value={block.author} placeholder="Author name" />
									</label>
								</If>
							</div>
						</article>
					</Each>
				</div>
			</main>
			<aside class="cms-preview" aria-label="Content preview">
				<h2 class="cms-panel-label">Preview</h2>
				<div class="cms-preview__canvas">
					<Each of={data.blocks} as="block">
						<div class={"cms-preview-block cms-preview-block--" + block.kind}>
							<If cond={block.kind == "hero"}>
								<div class="cms-preview-hero">
									<h1>{block.title}</h1>
									<span class="cms-preview-hero__divider"></span>
									<p>{block.subtitle}</p>
								</div>
							</If>
							<If cond={block.kind == "feature"}>
								<div class="cms-preview-feature">
									<h3>{block.title}</h3>
									<p>{block.body}</p>
								</div>
							</If>
							<If cond={block.kind == "quote"}>
								<figure class="cms-preview-quote">
									<blockquote>{block.text}</blockquote>
									<hr />
									<figcaption>— {block.author}</figcaption>
								</figure>
							</If>
						</div>
					</Each>
				</div>
			</aside>
		</div>
		<footer class="cms-statusbar">
			<span class="cms-statusbar__info">
				<span>{len(data.blocks)}</span> blocks in document
			</span>
		</footer>
	</div>
}
