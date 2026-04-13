package playground

func Page() Node {
	return <section class="play">
		<header class="play__header">
			<h1 class="play__title">GoSX Playground</h1>
			<p class="play__subtitle">Edit gsx on the left. Preview hydrates on the right.</p>
		</header>
		<div class="play__body">
			<div class="play__editor">
				<div class="play__editor-top">
					<label class="play__preset-label" for="play-preset-select">Preset</label>
					<select class="play__preset-select" id="play-preset-select" aria-label="Choose a preset">
						<Each of={data.presets} as="p">
							<option value={p.Slug}>{p.Title}</option>
						</Each>
					</select>
					<button class="play__reset-btn" type="button" aria-label="Reset to selected preset">Reset</button>
				</div>
				<textarea
					class="play__source"
					spellcheck="false"
					aria-label="gsx source"
				>{data.source}</textarea>
				<div class="play__errors" aria-live="polite"></div>
			</div>
			<div class="play__preview">
				<div class="play__preview-frame" data-gosx-island="playground-preview"></div>
				<div class="play__preview-status" aria-live="polite">Preview updates as you type</div>
			</div>
		</div>
		<details class="play__compiler">
			<summary>Compiler output</summary>
			<div class="play__compiler-body">Hydrate to see IR and program bytes.</div>
		</details>
	</section>
}
