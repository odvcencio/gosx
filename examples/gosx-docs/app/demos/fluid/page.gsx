package fluid

func Page() Node {
    return <section class="fluid" aria-label="Server-streamed velocity field">
    	<div class="fluid__frame">
    		<canvas
    			id="fluid-canvas"
    			class="fluid__canvas"
    			width={data.worldW}
    			height={data.worldH}
    			aria-label="particle flow canvas"
    		></canvas>
    		<div class="fluid__hud" aria-live="polite">
    			<div class="fluid__hud-title">VELOCITY FIELD</div>
    			<div class="fluid__stat">
    				<span class="fluid__stat-label">GRID</span>
    				<b class="fluid__stat-value" id="fluid-grid">
    					{data.gridN}
    					³
    				</b>
    			</div>
    			<div class="fluid__stat">
    				<span class="fluid__stat-label">BITS</span>
    				<b class="fluid__stat-value" id="fluid-bits">{data.bitWidth}</b>
    			</div>
    			<div class="fluid__stat">
    				<span class="fluid__stat-label">TICK</span>
    				<b class="fluid__stat-value" id="fluid-tick">—</b>
    			</div>
    			<div class="fluid__stat">
    				<span class="fluid__stat-label">WIRE</span>
    				<b class="fluid__stat-value" id="fluid-wire">—</b>
    			</div>
    			<div class="fluid__stat">
    				<span class="fluid__stat-label">RATE</span>
    				<b class="fluid__stat-value" id="fluid-rate">—</b>
    			</div>
    			<div class="fluid__stat">
    				<span class="fluid__stat-label">PARTICLES</span>
    				<b class="fluid__stat-value" id="fluid-particles">0</b>
    			</div>
    		</div>
    	</div>
    	<footer class="fluid__footer">
    		<span>
    			server advects a 16³ vec3 velocity field at 20 Hz · deltas quantized to 6 bits · particles ride the decoded slice client-side
    		</span>
    	</footer>
    	<script src="/fluid-client.js" defer></script>
    </section>
}
