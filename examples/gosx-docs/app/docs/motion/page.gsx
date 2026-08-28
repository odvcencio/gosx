package docs

func Page() Node {
	return <article class="prose">
		<section class="doc-scene" aria-labelledby={docScene.HeadingID}>
			<div id={docScene.SurfaceID} class="doc-scene__surface">
				<Scene3D class="doc-scene__mount" {...docScene.Scene} respectReducedMotion={true}>
					<div class="doc-scene__fallback">{docScene.Scene.UnsupportedMessage}</div>
				</Scene3D>
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
		<h2 id="bootstrap">Boundary with the motion package</h2>
		<p>
			<span class="inline-code">server.Motion</span>
			is the declarative DOM helper described here. The separate
			<span class="inline-code">motion</span>
			package contains clips, curves, mixing, springs, targets, and runtime evaluation for authored animation systems; those APIs are not automatically activated by adding a DOM preset.
		</p>
	</article>
}
