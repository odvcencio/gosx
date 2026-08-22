package docs

func Page() Node {
	return <section
		class="orrery"
		aria-label="Lodestar Meridian"
		role="region"
		data-gosx-scene3d-status-scope
		data-gosx-scene3d-control-scope
	>
		<div class="orrery__layout">
			<header class="orrery__intro">
				<p class="orrery__eyebrow">Declarative choreography · Scene3D</p>
				<h1>
					Lodestar Meridian
					<span>a clockwork star-system engine</span>
				</h1>
				<p class="orrery__copy">
					A levitating lodestone heart, three glass armatures, and three planets riding keyframed circles — every moving part is declared as Scene3D animation data and played by the shared GoSX scene runtime, with no page-authored animation code and zero assets.
				</p>
			</header>
			<div class="orrery__canvas">
				<Scene3D {...data.scene} />
			</div>
			<div class="orrery__overlay">
				<ol class="orrery__phases" aria-label="Demonstration cycle phases">
					<li>
						<span class="orrery__phase-time">0–3 s · ignition</span>
						The heart ramps from banked embers to cruise brightness.
					</li>
					<li>
						<span class="orrery__phase-time">always · procession</span>
						Three planets ride closed circular keys at 6 s, 8 s, and 12 s periods while the armatures precess.
					</li>
					<li>
						<span class="orrery__phase-time">12–14.4 s · transit</span>
						A dark moon crosses the heart's face; mid-transit at 13.2 s fires the flare and halo pulse on the same beat.
					</li>
					<li>
						<span class="orrery__phase-time">24 s · cycle</span>
						First and last keys match, so the loop never pops.
					</li>
				</ol>
				<div class="orrery__details">
					<div class="orrery__telemetry" aria-live="polite">
						<p class="orrery__runtime">
							<span>Renderer</span>
							<output data-gosx-scene3d-status="renderer">starting…</output>
							<output data-gosx-scene3d-status="fallback" hidden></output>
						</p>
						<p class="orrery__quality">
							<span>Quality</span>
							<output data-gosx-scene3d-status="quality">measuring…</output>
						</p>
					</div>
					<ul class="orrery__budgets" aria-label="Declared rendering and animation budgets">
						<li>17 stable nodes</li>
						<li>4 animation channels in 1 clip</li>
						<li>2 material key tracks</li>
						<li>220 seeded stars</li>
						<li>≤25k expanded vertices</li>
						<li>60 frames per second (FPS) cap</li>
						<li>device pixel ratio (DPR) ≤ 1.5</li>
						<li>720p render · 540p post FX</li>
						<li>512px shadow</li>
					</ul>
					<p class="orrery__motion-note">
						This scene animates continuously by design. Pause or resume the clockwork with the control below — the runtime freezes the scene clock exactly and continues from the same pose. Under the reduced-motion preference, GoSX suppresses this canvas's animation loop entirely and renders the composed opening pose as a still.
					</p>
					<button type="button" class="orrery__pause" data-gosx-scene3d-animation-toggle aria-pressed="false">
						<span class="orrery__pause-label orrery__pause-label-pause">Pause the clockwork</span>
						<span class="orrery__pause-label orrery__pause-label-play">Resume the clockwork</span>
					</button>
					<p class="orrery__controls">
						Drag or swipe to orbit · scroll or pinch to zoom
						<span>
							Keyboard: arrows explore · +/− zoom · Home restores the opening view
						</span>
					</p>
				</div>
			</div>
		</div>
	</section>
}
