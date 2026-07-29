package docs

func Page() Node {
	return <section class="html-surface" aria-label="HTML surfaces textured onto 3D geometry" role="region">
		<div class="html-surface__canvas">
			<Scene3D {...data.scene} />
		</div>
		<div class="html-surface__overlay">
			<p class="html-surface__eyebrow">
				Real HTML. Real CSS. On a surface in the scene.
			</p>
			<h1 class="html-surface__title">Diegetic panels</h1>
			<p class="html-surface__tagline">
				Each panel is a quad in the scene graph. The browser lays out the page markup and rasterizes it at device resolution. Orbit the scene: the panels rotate with it, occlude behind geometry, and stay legible off-axis.
			</p>
			<details class="html-surface__proof">
				<summary>
					How this differs from a document object model (DOM) overlay
				</summary>
				<ul>
					<li>
						The panel has a world transform. Rotation.X = -Pi/2 stands it up; Rotation.Y turns it away from the camera.
					</li>
					<li>
						Depth is real. The right-hand panel goes behind the pillar when you orbit past it.
					</li>
					<li>
						The markup is rasterized with the page's own stylesheets and webfonts, so a component renders the same on a page and on a panel.
					</li>
					<li>
						The DOM mirror stays in the accessibility tree but stops painting, so nothing draws twice.
					</li>
				</ul>
			</details>
		</div>
	</section>
}
