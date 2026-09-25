package docs

func Page() Node {
	return <section
		class="scene3d-showcase"
		aria-label="Geometry Zoo interactive material study"
		role="region"
		data-gosx-scene3d-status-scope
	>
		<div class="scene3d-showcase__canvas">
			<Scene3D {...data.scene} />
		</div>
		<div class="scene3d-showcase__overlay">
			<p class="scene3d-showcase__eyebrow">Material study / Scene3D</p>
			<h1 class="scene3d-showcase__title">Geometry Zoo</h1>
			<p class="scene3d-showcase__tagline">
				Seven surfaces under one light rig. Turn the scene to see how each material responds.
			</p>
			<p class="scene3d-showcase__runtime" aria-live="polite">
				<span>GoSX renderer</span>
				<output data-gosx-scene3d-status="renderer">starting…</output>
				<output data-gosx-scene3d-status="fallback" hidden></output>
			</p>
			<p class="scene3d-showcase__controls">
				Drag to orbit · scroll or pinch to zoom
			</p>
			<details class="scene3d-showcase__proof">
				<summary>What GoSX owns</summary>
				<ul>
					<li>
						Typed scene graph, stable mesh IDs, and per-mesh declarative spin
					</li>
					<li>
						PBR materials and a three-point lighting rig
					</li>
					<li>
						Shadow, bloom, vignette, color grade, and ACES passes
					</li>
					<li>
						Responsive orbit interaction and backend fallback
					</li>
				</ul>
				<a
					href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/demos/scene3d/program.go"
					target="_blank"
					rel="noopener noreferrer"
				>View the typed scene source</a>
			</details>
		</div>
	</section>
}
