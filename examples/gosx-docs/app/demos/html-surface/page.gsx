package docs

func Page() Node {
	return <section class="html-surface" aria-label="HTML surfaces textured onto 3D geometry" role="region">
		<div class="html-surface__canvas">
			<div class="html-surface__poster" aria-hidden="true">
				<div>
					<span>01 / STATUS</span>
					<strong>Coolant loop</strong>
					<em>Nominal</em>
				</div>
				<div>
					<span>02 / ANGLED</span>
					<strong>In the scene</strong>
					<em>HTML + CSS</em>
				</div>
			</div>
			<Scene3D {...data.scene} />
		</div>
		<div class="html-surface__overlay">
			<p class="html-surface__eyebrow">Scene study / HTML textures</p>
			<h1 class="html-surface__title">Interfaces with depth.</h1>
			<p class="html-surface__tagline">
				These panels start as HTML and CSS. Drag to orbit: each one turns with the scene and passes behind objects.
			</p>
			<details class="html-surface__proof">
				<summary>How the panels work</summary>
				<ul>
					<li>
						Each panel has a position and rotation in the 3D scene.
					</li>
					<li>
						The right panel passes behind the pillar as you orbit.
					</li>
					<li>
						The browser renders the markup, styles, and font into a texture.
					</li>
					<li>
						A hidden document mirror keeps the content available to assistive technology.
					</li>
				</ul>
			</details>
		</div>
	</section>
}
