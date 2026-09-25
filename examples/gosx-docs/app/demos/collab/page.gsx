package collab

func Page() Node {
	return <section
		class="collab"
		aria-label="Last-write-wins Hub synchronization demo with live presence and cursors"
		data-initial-version={data.initialVersion}
		data-view="write"
	>
		<header class="collab__header">
			<div class="collab__heading">
				<p class="collab__eyebrow">Live study / Hub sync</p>
				<h1 class="collab__title">Write together.</h1>
				<p>
					Open this page in another tab. Edits, presence, and cursors move between them.
				</p>
			</div>
			<div class="collab__session">
				<div class="collab__status" aria-live="polite">
					<span id="collab-status-dot" class="collab__status-dot"></span>
					<span id="collab-status" role="status">connecting…</span>
					<span aria-hidden="true">·</span>
					<span>
						v
						<span id="collab-version">{data.initialVersion}</span>
					</span>
					<span aria-hidden="true">·</span>
					<span id="collab-presence" class="collab__presence" role="status">connecting…</span>
				</div>
				<span id="collab-self" class="collab__self" aria-live="polite"></span>
			</div>
		</header>
		<div class="collab__mobile-tabs" aria-label="Editor view">
			<button type="button" data-collab-view="write" aria-pressed="true">Write</button>
			<button type="button" data-collab-view="preview" aria-pressed="false">Preview</button>
		</div>
		<div class="collab__body">
			<div class="collab__pane collab__pane--editor">
				<h2 class="collab__pane-label">Markdown</h2>
				<div class="collab__editor-wrap">
					<textarea id="collab-source" class="collab__source" spellcheck="false" aria-label="Markdown source">{data.initialText}</textarea>
					<div id="collab-cursors" class="collab__cursor-layer" aria-hidden="true"></div>
				</div>
			</div>
			<div class="collab__pane collab__pane--preview">
				<h2 class="collab__pane-label">Live preview</h2>
				<div id="collab-preview" class="collab__preview"></div>
			</div>
		</div>
		<footer class="collab__footer">
			<span>
				One shared document. The last accepted edit wins. This demo stores changes in memory only.
			</span>
		</footer>
		<script src="/collab-client.js" defer></script>
	</section>
}
