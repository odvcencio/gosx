package docs

func Page() Node {
	return <section class="orbital-study" aria-labelledby="orbital-study-title">
		<div
			class="orbital-study__scene gosx-scene3d-poster-stage"
			role="group"
			aria-label="Interactive orbital sculpture. Drag to orbit and scroll or pinch to zoom."
			data-gosx-scene3d-poster-stage
		>
			<img
				class="gosx-scene3d-poster"
				src="/demos/posters/showreel.webp"
				alt=""
				width="1000"
				height="625"
				decoding="async"
				fetchpriority="high"
			 />
			<Scene3D {...data.scene} stats={false} />
		</div>
		<div class="orbital-study__intro">
			<p class="orbital-study__eyebrow">A typed Scene3D study</p>
			<h1 id="orbital-study-title">Orbital sculpture</h1>
			<p>
				Turn a scene made from typed Go data. Move around its rings, core, and satellites.
			</p>
			<p class="orbital-study__hint">
				Drag to orbit · scroll or pinch to zoom
			</p>
			<a href="/demos" data-gosx-link="true">← All demos</a>
		</div>
	</section>
}
