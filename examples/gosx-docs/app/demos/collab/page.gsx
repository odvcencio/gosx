package collab

func Page() Node {
    return <section class="collab" aria-label="Collaborative markdown editor">
    	<header class="collab__header">
    		<h1 class="collab__title">Collab Editor</h1>
    		<div class="collab__status" aria-live="polite">
    			<span class="collab__status-dot"></span>
    			<span id="collab-status">connecting…</span>
    		</div>
    	</header>
    	<div class="collab__body">
    		<div class="collab__pane collab__pane--editor">
    			<div class="collab__pane-label">SOURCE</div>
    			<textarea id="collab-source" class="collab__source" spellcheck="false" aria-label="Markdown source">{data.initialText}</textarea>
    		</div>
    		<div class="collab__pane collab__pane--preview">
    			<div class="collab__pane-label">PREVIEW</div>
    			<div id="collab-preview" class="collab__preview" aria-live="polite"></div>
    		</div>
    	</div>
    	<footer class="collab__footer">
    		<span>
    			open this page in two tabs — edits sync via the hub in real time · LWW model, 100ms debounce
    		</span>
    	</footer>
    	<script src="/collab-client.js" defer></script>
    </section>
}
