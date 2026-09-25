package livesim

func Page() Node {
	return <section class="livesim" aria-label="Live 2D physics sandbox">
		<header class="livesim__heading">
			<div>
				<p class="livesim__eyebrow">Live study / shared simulation</p>
				<h1>Live Physics</h1>
				<p>
					Drop a circle into a shared world. Each tab sees the same server simulation.
				</p>
			</div>
			<span class="livesim__badge">20 updates / second</span>
		</header>
		<div class="livesim__frame">
			<div class="livesim__stage" id="livesim-stage" data-has-circles="false">
				<canvas
					id="livesim-canvas"
					class="livesim__canvas"
					width={data.worldW}
					height={data.worldH}
					aria-label="Physics canvas. Press Enter or Space to drop a circle."
					tabindex="0"
					role="application"
				></canvas>
				<div class="livesim__hint" aria-hidden="true">
					<span>↘</span>
					<strong>Your world starts here</strong>
					<small>Tap or click to drop a circle</small>
				</div>
				<p class="livesim__stage-note">LIVE / GO HUB</p>
			</div>
			<aside class="livesim__hud" aria-label="Simulation controls and statistics">
				<h2>World status</h2>
				<div class="livesim__stat">
					<span class="livesim__stat-label">Server tick</span>
					<b class="livesim__stat-value" id="livesim-frame">0</b>
				</div>
				<div class="livesim__stat">
					<span class="livesim__stat-label">Render</span>
					<b class="livesim__stat-value" id="livesim-render">—</b>
				</div>
				<div class="livesim__stat">
					<span class="livesim__stat-label">Circles</span>
					<b class="livesim__stat-value" id="livesim-count">0</b>
				</div>
				<div class="livesim__stat">
					<span class="livesim__stat-label">Connection</span>
					<b class="livesim__stat-value" id="livesim-state" role="status">connecting…</b>
				</div>
				<div class="livesim__stat">
					<span class="livesim__stat-label">Viewers</span>
					<b class="livesim__stat-value" id="livesim-viewers" role="status">1</b>
				</div>
				<div class="livesim__actions">
					<button class="livesim__spawn" id="livesim-spawn" type="button">Drop at center</button>
					<button class="livesim__burst" id="livesim-burst" type="button">Drop a cluster</button>
				</div>
			</aside>
		</div>
		<footer class="livesim__footer">
			<p>
				Use a pointer, touch, Enter, or Space to drop a circle. Open another tab to see the shared world and each viewer's cursor.
			</p>
			<p>
				The Go Hub sends physics updates at 20 Hz for up to
				{data.maxCircles}
				circles.
			</p>
		</footer>
		<script src="/livesim-client.js" defer></script>
	</section>
}
