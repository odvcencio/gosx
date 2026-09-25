package docs

func Page() Node {
	return <section class="orbital-study" aria-labelledby="orbital-study-title">
		<div
			class="orbital-study__scene"
			role="group"
			aria-label="Interactive orbital sculpture. Drag to orbit and scroll or pinch to zoom."
		>
			<Scene3D {...data.scene} />
		</div>
		<div class="orbital-study__intro">
			<p class="orbital-study__eyebrow">Scene3D / Study 01</p>
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
