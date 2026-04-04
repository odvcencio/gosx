package docs

func Page() Node {
	return <section class="geo-zoo" aria-label="Geometry Zoo interactive 3D demo">
		<div class="geo-zoo__scene">
			<Scene3D {...data.scene} />
		</div>
		<div class="geo-zoo__overlay">
			<h1 class="geo-zoo__title chrome-text">Geometry Zoo</h1>
			<p class="geo-zoo__subtitle">
				PBR geometry primitives — drag to orbit, scroll to zoom
			</p>
			<ul class="geo-zoo__legend" aria-label="Geometry types in scene">
				<li>Sphere</li>
				<li>Box</li>
				<li>Pyramid</li>
				<li>Cylinder</li>
				<li>Torus</li>
				<li>Sphere (clay)</li>
			</ul>
		</div>
	</section>
}
