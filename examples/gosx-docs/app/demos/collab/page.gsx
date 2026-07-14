package collab

func Page() Node {
	    return <section
	    	class="collab"
	    	aria-label="Last-write-wins Hub synchronization demo with live presence and cursors"
	    	data-initial-version={data.initialVersion}
	    >
	    	<header class="collab__header">
	    		<h1 class="collab__title">Hub Sync · LWW</h1>
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
	    	</header>
	    	<div class="collab__body">
	    		<div class="collab__pane collab__pane--editor">
	    			<div class="collab__pane-label">SOURCE</div>
	    			<div class="collab__editor-wrap">
	    				<textarea id="collab-source" class="collab__source" spellcheck="false" aria-label="Markdown source">{data.initialText}</textarea>
	    				<div id="collab-cursors" class="collab__cursor-layer" aria-hidden="true"></div>
	    			</div>
	    		</div>
	    		<div class="collab__pane collab__pane--preview">
	    			<div class="collab__pane-label">PREVIEW</div>
	    			<div id="collab-preview" class="collab__preview"></div>
	    		</div>
	    	</div>
	    	<footer class="collab__footer">
	    		<span>
	    			one shared in-memory document · last write wins · live presence and remote cursors over the hub · no CRDT, rooms, or persistence
	    		</span>
	    	</footer>
	    	<script src="/collab-client.js" defer></script>
	    </section>
}
