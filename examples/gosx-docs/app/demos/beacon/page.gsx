package docs

func Page() Node {
	return <section
		class="beacon"
		aria-label="Blackglass Coast"
		role="region"
		data-gosx-scene3d-status-scope
	>
		<div class="beacon__canvas">
			<Scene3D {...data.scene} />
		</div>
		<div class="beacon__overlay">
			<p class="beacon__eyebrow">Studio world contract · Scene3D</p>
			<h1>
				Blackglass Coast
				<span>sunlit volcanic water world</span>
			</h1>
			<p class="beacon__copy">
				A Studio-authored cove becomes a live GoSX water world: glassy surf, basalt shelves, weathered ruins, and an ember beacon on the far terrace.
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
				<li>16 stable nodes</li>
				<li>12 instanced basalt forms</li>
				<li>128² water simulation</li>
				<li>60 frames per second (FPS) cap</li>
				<li>device pixel ratio (DPR) ≤ 1.5</li>
				<li>720p render</li>
				<li>540p post FX</li>
				<li>~16.7 ms adaptive target</li>
				<li>512px shadow</li>
			</ul>
			<p class="beacon__controls">
				Drag to orbit · scroll or pinch to zoom
			</p>
		</div>
	</section>
}
