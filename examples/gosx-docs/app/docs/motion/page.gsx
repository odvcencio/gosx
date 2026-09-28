package docs

func Page() Node {
	return <article class="prose motion-page">
		<section class="docs-live-example" aria-label="Motion preset example">
			<p class="eyebrow">Server-authored motion</p>
			{motionExample}
			<p>
				The example respects reduced-motion settings and keeps its HTML visible without JavaScript.
			</p>
			<a
				href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/docs/motion/page.server.go"
				rel="noopener"
			>View the Go motion setup</a>
		</section>
		<section
			class="motion-demo__layout"
			aria-labelledby={docScene.HeadingID}
			data-gosx-motion-program={data.motionProgram}
		>
			<div
				id={docScene.SurfaceID}
				class="motion-demo__surface"
				role="region"
				aria-label="Scene3D motion demo"
			>
				<div class="motion-demo__stage">
					<Scene3D id="motion-scene" class="motion-demo__mount" {...docScene.Scene} respectReducedMotion={true}>
						<div class="motion-demo__fallback">{docScene.Scene.UnsupportedMessage}</div>
					</Scene3D>
					<div class="motion-demo__overlay">
						<article id="motion-card" class="motion-demo__card">
							<p class="motion-demo__label">One scroll value</p>
							<h2>HTML and the camera move together.</h2>
							<p>
								Scroll the page. The card and camera read the same progress.
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
				<div id="motion-scroll-track" class="motion-demo__scroll-track" aria-hidden="true"></div>
			</div>
			<div class="motion-demo__teaching">
				<p class="motion-demo__eyebrow">{docScene.Eyebrow}</p>
				<p id={docScene.HeadingID} class="motion-demo__title" role="heading" aria-level="2">{docScene.Title}</p>
				<p class="motion-demo__summary">{docScene.Summary}</p>
				<dl class="motion-demo__facts">
					<div>
						<dt>Backend contract</dt>
						<dd>{docScene.BackendTruth}</dd>
					</div>
					<div>
						<dt>Interaction</dt>
						<dd>{docScene.InteractionHint}</dd>
					</div>
				</dl>
				<a href={docScene.DemoHref} data-gosx-link="true" class="motion-demo__link">{docScene.DemoLabel}</a>
			</div>
		</section>
		<div class="page-topper">
			<span class="eyebrow">Progressive DOM motion</span>
			<p class="lede">
				The server motion primitive emits semantic HTML and a small set of bootstrap-managed transition attributes. Reduced-motion respect is the default.
			</p>
		</div>
		<h2 id="dom-motion">DOM motion</h2>
		<CodeBlock lang="go" source={data.motionSample} />
		<p>
			Use
			<span class="inline-code">ctx.Motion</span>
			,
			<span class="inline-code">ctx.Runtime().Motion</span>
			, or the page-state helper so the document bootstrap is enabled. The package-level
			<span class="inline-code">server.Motion</span>
			only creates the element and attributes; it cannot activate page assets by itself.
		</p>
		<h2 id="presets">Presets</h2>
		<p>
			The DOM presets are
			<span class="inline-code">fade</span>
			,
			<span class="inline-code">slide-up</span>
			,
			<span class="inline-code">slide-down</span>
			,
			<span class="inline-code">slide-left</span>
			,
			<span class="inline-code">slide-right</span>
			, and
			<span class="inline-code">zoom-in</span>
			. Unknown values normalize to fade.
		</p>
		<h2 id="triggers">Load and viewport triggers</h2>
		<p>
			<span class="inline-code">MotionTriggerLoad</span>
			is the default and starts when the bootstrap initializes the element.
			<span class="inline-code">MotionTriggerView</span>
			waits for the framework's viewport observation. The server HTML remains the fallback when scripting is unavailable.
		</p>
		<h2 id="reduced-motion">Reduced motion</h2>
		<p>
			<span class="inline-code">RespectReducedMotion</span>
			is a pointer because omission means true. The bootstrap consults the user's reduced-motion preference and suppresses the authored transition when respect is enabled.
		</p>
		<CodeBlock lang="go" source={data.reducedSample} />
		<p>
			Opting out is possible for an essential transition, but it should be deliberate and rare. The GSX
			<span class="inline-code">respectReducedMotion</span>
			property maps to the same contract.
		</p>
		<h2 id="timing">Timing defaults</h2>
		<p>
			Duration defaults to 220 milliseconds, delay to zero, distance to 18 pixels, and easing to
			<span class="inline-code">cubic-bezier(0.16, 1, 0.3, 1)</span>
			. Non-positive duration or distance selects the default; negative delay becomes zero.
		</p>
		<h2 id="one-program">One shared program</h2>
		<p>
			The motion package shares scroll, hover, and spring values between DOM styles and Scene3D properties. A spring keeps its velocity when a new target arrives, so a quick pointer change continues smoothly.
		</p>
		<CodeBlock lang="go" source={data.programSample} />
		<h2 id="bootstrap">Boundary with the motion package</h2>
		<p>
			<span class="inline-code">server.Motion</span>
			is the declarative DOM helper described here. The separate
			<span class="inline-code">motion</span>
			package contains clips, curves, mixing, springs, targets, and runtime evaluation for authored animation systems; those APIs are not automatically activated by adding a DOM preset.
		</p>
	</article>
}
