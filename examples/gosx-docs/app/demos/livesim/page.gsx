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
			tabindex="0"
			role="application"
		></canvas>
		<div class="livesim__hud">
			<div class="livesim__stat">
				<span class="livesim__stat-label">SERVER TICK</span>
				<b class="livesim__stat-value" id="livesim-frame">0</b>
			</div>
			<div class="livesim__stat">
				<span class="livesim__stat-label">RENDER</span>
				<b class="livesim__stat-value" id="livesim-render">—</b>
			</div>
			<div class="livesim__stat">
				<span class="livesim__stat-label">CIRCLES</span>
				<b class="livesim__stat-value" id="livesim-count">0</b>
			</div>
			<div class="livesim__stat">
				<span class="livesim__stat-label">STATE</span>
				<b class="livesim__stat-value" id="livesim-state" role="status">connecting…</b>
			</div>
			<button class="livesim__spawn" id="livesim-spawn" type="button">Spawn at center</button>
		</div>
	</div>
	<footer class="livesim__footer">
		<span>
			pointer, touch, or Enter/Space spawns a circle · GoSX Sim owns state at 20 Hz · browser interpolates snapshots
		</span>
	</footer>
	<script src="/livesim-client.js" defer></script>
    </section>
}
