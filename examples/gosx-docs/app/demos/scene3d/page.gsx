package docs

func Page() Node {
	return <section
		class="scene3d-showcase"
		aria-label="Scene3D cinematic PBR showcase — interactive 3D demo"
		role="region"
	>
		<div class="scene3d-showcase__canvas">
			<Scene3D {...data.scene} />
		</div>
		<div class="scene3d-showcase__overlay" aria-hidden="true">
			<h1 class="scene3d-showcase__title">Scene3D</h1>
			<p class="scene3d-showcase__tagline">
				Cinematic PBR · ACES tonemap · HDR bloom
			</p>
			<p class="scene3d-showcase__controls">
				DRAG ORBIT · SCROLL ZOOM
			</p>
		</div>
	</section>
}
