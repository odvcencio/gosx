package docs

func CMSMetric(props any) Node {
	return <article class="signal-badge">
		<strong>{props.Value}</strong>
		<p>{props.Label}</p>
	</article>
}

func CMSPaletteCard(props any) Node {
	return <article class="cms-palette-card" data-cms-palette-card data-cms-type={props.Type} draggable="true">
		<div class="cms-palette-card__head">
			<span class="eyebrow">{props.Kicker}</span>
			<button type="button" class="chip" data-cms-add-type={props.Type}>Add</button>
		</div>
		<h3>{props.Title}</h3>
		<p>{props.Body}</p>
	</article>
}

func CMSEditorBlock(props any) Node {
	return <article class={"cms-block cms-block--" + props.Type} data-cms-block data-block-id={props.Id}>
		<div class="cms-block__head">
			<div class="cms-block__meta">
				<span class="eyebrow">{props.Type} block</span>
				<p>Drag to reorder. Edits sync into the preview immediately.</p>
			</div>
			<div class="cms-block__actions">
				<button type="button" class="chip" data-cms-move={props.Id} data-cms-direction="up">Up</button>
				<button type="button" class="chip" data-cms-move={props.Id} data-cms-direction="down">Down</button>
				<span class="chip cms-drag-handle" data-cms-drag-handle data-block-id={props.Id} draggable="true">Drag</span>
				<button type="button" class="chip" data-cms-remove={props.Id}>Remove</button>
			</div>
		</div>
		<div class="cms-block__fields">
			<If when={props.Type == "hero"}>
				<>
					<label class="field">
						<span>Eyebrow</span>
						<input type="text" value={props.Eyebrow} placeholder="Feature launch" data-cms-field="eyebrow" />
					</label>
					<label class="field">
						<span>Headline</span>
						<input type="text" value={props.Title} placeholder="Launch headline" data-cms-field="title" />
					</label>
					<label class="field">
						<span>Body</span>
						<textarea rows="4" placeholder="Hero copy" data-cms-field="body">{props.Body}</textarea>
					</label>
					<label class="field">
						<span>CTA label</span>
						<input type="text" value={props.Cta} placeholder="Start the release" data-cms-field="cta" />
					</label>
				</>
			</If>
			<If when={props.Type == "feature"}>
				<>
					<label class="field">
						<span>Stat</span>
						<input type="text" value={props.Stat} placeholder="03" data-cms-field="stat" />
					</label>
					<label class="field">
						<span>Title</span>
						<input type="text" value={props.Title} placeholder="Feature headline" data-cms-field="title" />
					</label>
					<label class="field">
						<span>Body</span>
						<textarea rows="4" placeholder="Feature copy" data-cms-field="body">{props.Body}</textarea>
					</label>
				</>
			</If>
			<If when={props.Type == "quote"}>
				<>
					<label class="field">
						<span>Quote</span>
						<textarea rows="5" placeholder="Quote body" data-cms-field="body">{props.Body}</textarea>
					</label>
					<label class="field">
						<span>Attribution</span>
						<input type="text" value={props.Attribution} placeholder="Editorial desk" data-cms-field="attribution" />
					</label>
				</>
			</If>
		</div>
	</article>
}

func CMSPreviewBlock(props any) Node {
	return <>
		<If when={props.Type == "hero"}>
			<section class="cms-preview-block cms-preview-block--hero">
				<span class="eyebrow">{props.Eyebrow}</span>
				<h3>{props.Title}</h3>
				<p>{props.Body}</p>
				<span class="chip">{props.Cta}</span>
			</section>
		</If>
		<If when={props.Type == "feature"}>
			<section class="cms-preview-block cms-preview-block--feature">
				<span class="cms-preview-block__stat">{props.Stat}</span>
				<div>
					<h3>{props.Title}</h3>
					<p>{props.Body}</p>
				</div>
			</section>
		</If>
		<If when={props.Type == "quote"}>
			<blockquote class="cms-preview-block cms-preview-block--quote">
				<p>{props.Body}</p>
				<footer>{props.Attribution}</footer>
			</blockquote>
		</If>
	</>
}

func Page() Node {
	return <article class="cms-shell">
		<section class="page-topper">
			<span class="eyebrow">CMS Demo</span>
			<p class="lede">
				Drag blocks into the draft, edit them live, and publish the document through one routed action.
			</p>
		</section>
		<section class="cms-hero">
			<div class="cms-hero__copy">
				<h1>The CMS flow stays document-shaped. Compose once, publish once.</h1>
				<p>
					This route is not a detached admin app. The editor, preview, form action, flashed result, and final product shell all live inside the same GoSX
					application model.
				</p>
				<div class="hero-actions">
					<Link class="hero-link primary" href="/docs/forms">Inspect the action model</Link>
					<Link class="hero-link" href="/demos/scene3d">Open the 3D showcase</Link>
				</div>
			</div>
			<aside class="home-hero__meta">
				<Each as="metric" of={data.metrics}>
					<CMSMetric {...metric}></CMSMetric>
				</Each>
			</aside>
		</section>
		<section class="cms-workbench" data-cms-demo data-cms-document={data.documentJSON}>
			<div class="cms-toolbar">
				<div class="cms-toolbar__copy">
					<span class="eyebrow">Editor state</span>
					<h2>
						<span data-cms-count>{len(data.document.blocks)}</span>
						blocks live in the draft and
						<span data-cms-words>0</span>
						words feed the preview.
					</h2>
					<p class="form-status" aria-live="polite">{action.message}</p>
					<p class="flash-note" aria-live="polite">{flash.notice}</p>
					<p class="form-error">{actions.publish.fieldErrors.document}</p>
				</div>
				<form class="cms-publish-form" method="post" action={request.path + "/__actions/publish"}>
					<input type="hidden" name="csrf_token" value={csrf.token}></input>
					<input type="hidden" name="document" value={data.documentJSON} data-cms-input></input>
					<button class="cta-link primary" type="submit">Publish draft</button>
				</form>
			</div>
			<div class="cms-studio">
				<aside class="cms-panel">
					<div class="cms-panel__head">
						<span class="eyebrow">Palette</span>
						<h2>Block elements</h2>
						<p>Drag one into the draft or click add.</p>
					</div>
					<div class="cms-palette">
						<CMSPaletteCard
							type="hero"
							kicker="Hero"
							title="Launch section"
							body="Big headline, supporting copy, and one clear CTA."
						></CMSPaletteCard>
						<CMSPaletteCard
							type="feature"
							kicker="Feature"
							title="Signal card"
							body="One stat, one title, and supporting explanation for the main story."
						></CMSPaletteCard>
						<CMSPaletteCard
							type="quote"
							kicker="Quote"
							title="Editorial pull quote"
							body="Add a testimonial or an internal product note without leaving the page flow."
						></CMSPaletteCard>
					</div>
				</aside>
				<section class="cms-panel">
					<div class="cms-panel__head">
						<span class="eyebrow">Composer</span>
						<h2>Live draft</h2>
						<p>The editor writes the whole document payload, not isolated widget state.</p>
					</div>
					<div class="cms-block-list" data-cms-block-list>
						<If when={len(data.document.blocks) == 0}>
							<div class="cms-empty">
								<strong>No blocks yet.</strong>
								<p>Drag one from the palette to start the page.</p>
							</div>
						</If>
						<If when={len(data.document.blocks) > 0}>
							<Each as="block" of={data.document.blocks}>
								<CMSEditorBlock {...block}></CMSEditorBlock>
							</Each>
						</If>
					</div>
				</section>
				<aside class="cms-panel">
					<div class="cms-panel__head">
						<span class="eyebrow">Preview</span>
						<h2>Published surface</h2>
						<p>The right rail is the document the publish action will receive.</p>
					</div>
					<div class="cms-preview-canvas" data-cms-preview>
						<If when={len(data.document.blocks) == 0}>
							<div class="cms-empty">
								<strong>The preview is waiting.</strong>
								<p>Add a block to populate the published page.</p>
							</div>
						</If>
						<If when={len(data.document.blocks) > 0}>
							<Each as="block" of={data.document.blocks}>
								<CMSPreviewBlock {...block}></CMSPreviewBlock>
							</Each>
						</If>
					</div>
				</aside>
			</div>
		</section>
		<section class="callout">
			<strong>What this demo is proving</strong>
			<p>
				The authoring layer can be rich and immediate without abandoning boring browser forms. The draft is just JSON in a hidden input, and publish is still a
				colocated server action.
			</p>
		</section>
	</article>
}
