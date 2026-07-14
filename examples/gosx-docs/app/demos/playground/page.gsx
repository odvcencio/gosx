package playground

func Page() Node {
	return <section class="play" data-compile-url={actionPath("compile")} data-csrf-token={csrf.token}>
		<header class="play__header">
			<h1 class="play__title">GoSX Playground</h1>
			<p class="play__subtitle">
				Edit gsx on the left. Preview hydrates on the right.
			</p>
		</header>
		<div class="play__body">
			<div class="play__editor">
				<div class="play__editor-top">
					<label class="play__preset-label" for="play-preset-select">Preset</label>
					<select
						class="play__preset-select"
						id="play-preset-select"
						aria-label="Choose a preset"
						aria-describedby="play-preset-description"
					>
						<Each of={data.presets} as="p">
							<option value={p.Slug} data-source={p.Source} data-description={p.Description}>{p.Title}</option>
						</Each>
					</select>
					<button class="play__reset-btn" type="button" aria-label="Reset to selected preset">Reset</button>
				</div>
				<p class="play__preset-description" id="play-preset-description">{data.presets[0].Description}</p>
				<textarea
					class="play__source"
					spellcheck="false"
					aria-label="gsx source"
					title="Ctrl+Enter compiles immediately"
				>{data.source}</textarea>
				<div class="play__editor-meta">
					<span class="play__editor-meta-item" data-editor-stat="lines">
						{data.initialLines}
						lines
					</span>
					<span class="play__editor-meta-item" data-editor-stat="chars">
						{data.initialChars}
						chars
					</span>
					<span class="play__editor-meta-item play__editor-meta-hint">Ctrl+Enter compiles now</span>
				</div>
				<div class="play__errors" aria-live="polite"></div>
			</div>
			<div class="play__preview">
				<div class="play__preview-frame">{data.preview}</div>
				<div class="play__preview-status" aria-live="polite">Preview updates as you type</div>
			</div>
		</div>
		<details class="play__compiler">
			<summary>Compiler output</summary>
			<div class="play__compiler-body">
				<div class="play__stat-strip">
					<div class="play__stat">
						<span class="play__stat-label">Latency</span>
						<b class="play__stat-value" data-stat="latency">—</b>
					</div>
					<div class="play__stat">
						<span class="play__stat-label">Program</span>
						<b class="play__stat-value" data-stat="bytes">
							{data.initialProgramBytes}
							bytes
						</b>
					</div>
					<div class="play__stat">
						<span class="play__stat-label">Nodes</span>
						<b class="play__stat-value" data-stat="nodes">{data.initialNodeCount}</b>
					</div>
					<div class="play__stat">
						<span class="play__stat-label">Exprs</span>
						<b class="play__stat-value" data-stat="exprs">{data.initialExprCount}</b>
					</div>
					<div class="play__stat">
						<span class="play__stat-label">Diagnostics</span>
						<b class="play__stat-value" data-stat="diagnostics">{data.initialDiagnostics}</b>
					</div>
				</div>
			</div>
		</details>
		<script src="/playground-editor.js" defer></script>
	</section>
}
