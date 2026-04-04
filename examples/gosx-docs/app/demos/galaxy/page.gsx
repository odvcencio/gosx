package galaxy

func Page() Node {
	return <article class="galaxy-shell">
		<section class="galaxy-stage">
			<Scene3D class="galaxy-scene" {...data.scene}>
				<div class="scene-fallback">Loading galaxy...</div>
			</Scene3D>
		</section>
		<section class="galaxy-copy">
			<h1>No three.js. No dependencies. Just GoSX.</h1>
			<p>
				2,800 particles rendered as native GL_POINTS with additive blending,
				size attenuation, exponential fog, and a two-arm spiral — all authored
				in Go and owned by the server route.
			</p>
		</section>
	</article>
}
