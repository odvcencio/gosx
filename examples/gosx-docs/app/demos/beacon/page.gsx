package docs

func Page() Node {
	return <section class="beacon" aria-label="Blackglass Beacon — Eclipse Protocol" role="region">
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
					<output>
						<span class="beacon__backend beacon__backend--starting">starting…</span>
						<span class="beacon__backend beacon__backend--webgpu">WebGPU</span>
						<span class="beacon__backend beacon__backend--webgl">WebGL2</span>
						<span class="beacon__backend beacon__backend--other">bounded fallback</span>
						<span class="beacon__fallback">· fallback active</span>
					</output>
				</p>
				<p class="beacon__quality">
					<span>Quality</span>
					<output>
						<span class="beacon__tier beacon__tier--starting">measuring…</span>
						<span class="beacon__tier beacon__tier--full">full</span>
						<span class="beacon__tier beacon__tier--balanced">balanced</span>
						<span class="beacon__tier beacon__tier--survival">survival</span>
						<span class="beacon__tier beacon__tier--fixed">fixed</span>
					</output>
				</p>
			</div>
			<ul class="beacon__budgets" aria-label="Declared rendering budgets">
				<li>19 nodes</li>
				<li>&lt;40k vertices</li>
				<li>zero asset bytes</li>
				<li>30 FPS cap</li>
				<li>DPR ≤ 1.5</li>
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
