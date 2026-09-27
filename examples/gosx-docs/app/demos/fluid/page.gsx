package fluid

func Page() Node {
	return <section class="fluid" aria-label="Server-streamed velocity field">
		<header class="fluid__intro">
			<p>A live field streamed from Go</p>
			<h1>Velocity Field</h1>
			<span>
				Watch particles trace a field computed on the server. Drag across the canvas to bend their path.
			</span>
		</header>
		<div class="fluid__frame">
			<canvas
				id="fluid-canvas"
				class="fluid__canvas"
				width={data.worldW}
				height={data.worldH}
				aria-label="particle flow canvas"
			></canvas>
			<div class="fluid__hud">
				<div class="fluid__hud-title">Field details</div>
				<div class="fluid__stat">
					<span class="fluid__stat-label">State</span>
					<b class="fluid__stat-value" id="fluid-state" role="status">connecting…</b>
				</div>
				<div class="fluid__stat">
					<span class="fluid__stat-label">Grid size</span>
					<b class="fluid__stat-value" id="fluid-grid">
						{data.gridN}
						³
					</b>
				</div>
				<div class="fluid__stat">
					<span class="fluid__stat-label">Bits per value</span>
					<b class="fluid__stat-value" id="fluid-bits">{data.bitWidth}</b>
				</div>
				<div class="fluid__stat">
					<span class="fluid__stat-label">Tick</span>
					<b class="fluid__stat-value" id="fluid-tick">—</b>
				</div>
				<div class="fluid__stat">
					<span class="fluid__stat-label">Wire size</span>
					<b class="fluid__stat-value" id="fluid-wire">—</b>
				</div>
				<div class="fluid__stat">
					<span class="fluid__stat-label">Compression</span>
					<b class="fluid__stat-value" id="fluid-compression">—</b>
				</div>
				<div class="fluid__stat">
					<span class="fluid__stat-label">Frame</span>
					<b class="fluid__stat-value" id="fluid-frame-kind">waiting</b>
				</div>
				<div class="fluid__stat">
					<span class="fluid__stat-label">Rate</span>
					<b class="fluid__stat-value" id="fluid-rate">—</b>
				</div>
				<div class="fluid__stat">
					<span class="fluid__stat-label">Particles</span>
					<b class="fluid__stat-value" id="fluid-particles">0</b>
				</div>
			</div>
		</div>
		<footer class="fluid__footer">
			<span>
				The server sends a compact field at 20 Hz. Your drag changes only this browser's particle view.
			</span>
		</footer>
		<script src="/fluid-client.js" defer></script>
	</section>
}
