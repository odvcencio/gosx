package docs

func Page() Node {
	return <section
		class="beacon"
		aria-label="Blackglass Beacon — Eclipse Protocol"
		role="region"
		data-gosx-scene3d-status-scope
	>
		<div class="beacon__canvas">
			<Scene3D {...data.scene} />
		</div>
		<div class="beacon__overlay">
			<p class="beacon__eyebrow">Signal 07 · asset-free Scene3D</p>
			<h1>
				Blackglass Beacon
				<span>— Eclipse Protocol</span>
			</h1>
			<p class="beacon__copy">
				A cut-black tower holds a cyan eclipsed lantern above a warm containment core. The diagonal beam is fixed geometry, not a post-production effect.
			</p>
			<div class="beacon__telemetry" aria-live="polite">
				<p class="beacon__runtime">
					<span>Renderer</span>
					<output data-gosx-scene3d-status="renderer">starting…</output>
					<output data-gosx-scene3d-status="fallback" hidden></output>
				</p>
				<p class="beacon__quality">
					<span>Quality</span>
					<output data-gosx-scene3d-status="quality">measuring…</output>
				</p>
			</div>
			<ul class="beacon__budgets" aria-label="Declared rendering budgets">
				<li>19 nodes</li>
				<li>&lt;40k vertices</li>
				<li>zero asset bytes</li>
				<li>30 frames per second (FPS) cap</li>
				<li>device pixel ratio (DPR) ≤ 1.5</li>
				<li>720p render</li>
				<li>540p post FX</li>
				<li>~24 ms adaptive target</li>
				<li>512px shadow</li>
			</ul>
			<p class="beacon__controls">
				Drag to orbit · scroll or pinch to zoom
			</p>
		</div>
	</section>
}
