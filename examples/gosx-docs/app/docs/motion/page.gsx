package docs

func Page() Node {
	return <article class="prose motion-page">
		<section
			class="doc-scene"
			aria-labelledby={docScene.HeadingID}
			data-gosx-motion-program={data.motionProgram}
		>
			<div
				id={docScene.SurfaceID}
				class="doc-scene__surface motion-demo__surface"
				role="region"
				tabindex="0"
				aria-label="Scrollable Scene3D motion demo"
			>
				<div class="motion-demo__stage">
					<Scene3D id="motion-scene" class="doc-scene__mount" {...docScene.Scene} respectReducedMotion={true}>
						<div class="doc-scene__fallback">{docScene.Scene.UnsupportedMessage}</div>
					</Scene3D>
					<div class="motion-demo__overlay">
						<article id="motion-card" class="motion-demo__card">
							<p class="motion-demo__label">One scroll value</p>
							<h2>HTML and the camera move together.</h2>
							<p>
								Scroll inside this panel. The card and camera read the same progress.
							</p>
						</article>
						<button id="motion-hover" class="motion-demo__button" type="button">Hover or focus to move the sphere</button>
						<div
							id="motion-pin-anchor"
							class="motion-demo__pin"
							aria-label="The amber scene node follows this marker"
						>
							<span aria-hidden="true"></span>
							<p>HTML marker</p>
						</div>
						<output
							class="motion-demo__adapter"
							data-gosx-motion-adapter-report="#motion-scene"
							aria-live="polite"
						>Waiting for the scene adapter…</output>
					</div>
				</div>
				<div class="motion-demo__scroll-track" aria-hidden="true"></div>
			</div>
			<div class="doc-scene__teaching">
				<p class="doc-scene__eyebrow">{docScene.Eyebrow}</p>
				<p id={docScene.HeadingID} class="doc-scene__title" role="heading" aria-level="2">{docScene.Title}</p>
				<p class="doc-scene__summary">{docScene.Summary}</p>
				<dl class="doc-scene__facts">
					<div>
						<dt>Backend contract</dt>
						<dd>{docScene.BackendTruth}</dd>
					</div>
					<div>
						<dt>Interaction</dt>
						<dd>{docScene.InteractionHint}</dd>
					</div>
				</dl>
				<a href={docScene.DemoHref} data-gosx-link="true" class="doc-scene__link">{docScene.DemoLabel}</a>
			</div>
		</section>
		<div class="page-topper">
			<span class="eyebrow">Motion values across the page</span>
			<p class="lede">
				The HTML card, hover spring, and pinned sphere share values with one Scene3D render. Try the button, then scroll the scene panel.
			</p>
		</div>
		<p>
			The server serializes this graph once. In the browser, scroll input, the hover spring, DOM writes, and Scene3D rendering pass through one frame scheduler. The HTML and scene read the same signals.
		</p>
		<h2 id="one-program">Author one program</h2>
		<p>
			Start with the values and bind each one to the places that use it. A spring keeps its current velocity when a new target arrives, so a quick pointer change does not restart from rest.
		</p>
		<CodeBlock lang="go" source={data.programSample} />
		<h2 id="presets">Presets and reduced motion</h2>
		<p>
			The existing
			<span class="inline-code">Motion</span>
			presets still work for small DOM reveals. Reduced motion is enabled by default; choose
			<span class="inline-code">skip</span>
			,
			<span class="inline-code">fade</span>
			, or
			<span class="inline-code">static</span>
			for each animation. The HTML stays readable when an animation cannot start.
		</p>
		<CodeBlock lang="go" source={data.motionSample} />
		<p>
			For larger interactions, bind a signal to a DOM style, a CSS variable, a scene node, a material uniform, or the camera. Use
			<span class="inline-code">PinTo</span>
			when a scene node should follow an element's measured position.
		</p>
	</article>
}
