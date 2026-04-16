package livesim

func Page() Node {
    return <section class="livesim" aria-label="Live 2D physics sandbox">
    	<div class="livesim__frame">
    		<canvas
    			id="livesim-canvas"
    			class="livesim__canvas"
    			width={data.worldW}
    			height={data.worldH}
    			aria-label="physics canvas — click to drop a circle"
    		></canvas>
    		<div class="livesim__hud" aria-live="polite">
    			<div class="livesim__stat">
    				<span class="livesim__stat-label">FRAME</span>
    				<b class="livesim__stat-value" id="livesim-frame">0</b>
    			</div>
    			<div class="livesim__stat">
    				<span class="livesim__stat-label">CIRCLES</span>
    				<b class="livesim__stat-value" id="livesim-count">0</b>
    			</div>
    			<div class="livesim__stat">
    				<span class="livesim__stat-label">STATE</span>
    				<b class="livesim__stat-value" id="livesim-state">connecting…</b>
    			</div>
    		</div>
    	</div>
    	<footer class="livesim__footer">
    		<span>
    			click anywhere to drop a circle · state is server-authoritative · tick 20 Hz
    		</span>
    	</footer>
    	<script src="/livesim-client.js" defer></script>
    </section>
}
