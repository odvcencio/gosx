package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Motion</span>
			<p class="lede">
				GoSX now exposes DOM motion as a framework primitive instead of forcing every page to hand-roll animation glue.
			</p>
		</div>
		<h1>
			<span class="inline-code">Motion</span>
			ships server-authored animation presets on the shared bootstrap layer.
		</h1>
		<p>
			The
			<span class="inline-code">&lt;Motion /&gt;</span>
			builtin,
			<span class="inline-code">server.Motion(...)</span>
			, and
			<span class="inline-code">ctx.Motion(...)</span>
			all render normal HTML first, then the lightweight bootstrap runtime upgrades those elements into managed entrance motion when JavaScript is available. That keeps authored content server-first while giving the framework one place to own easing, timing, viewport triggers, and reduced-motion policy.
		</p>
		<section class="feature-grid">
			<div class="card">
				<strong>Server-authored HTML</strong>
				<p>
					Your page still renders a real DOM element on the server. Motion is enhancement, not a separate rendering model.
				</p>
			</div>
			<div class="card">
				<strong>Preset-driven</strong>
				<p>
					Use
					<span class="inline-code">fade</span>
					,
					<span class="inline-code">slide-up</span>
					,
					<span class="inline-code">slide-down</span>
					,
					<span class="inline-code">slide-left</span>
					,
					<span class="inline-code">slide-right</span>
					, or
					<span class="inline-code">zoom-in</span>
					without rewriting keyframes per page.
				</p>
			</div>
			<div class="card">
				<strong>Load or viewport triggers</strong>
				<p>
					Choose
					<span class="inline-code">load</span>
					for page-entry motion or
					<span class="inline-code">view</span>
					to defer the animation until the element intersects the viewport.
				</p>
			</div>
			<div class="card">
				<strong>Reduced-motion aware</strong>
				<p>
					The bootstrap layer respects
					<span class="inline-code">prefers-reduced-motion</span>
					by default so motion stays declarative without ignoring accessibility policy.
				</p>
			</div>
		</section>
		<h2>
			Author the same contract in
			<span class="inline-code">.gsx</span>
			or Go
		</h2>
		{DocsCodeBlock("gosx", `func Page() Node {
	    return <Motion
	        as="section"
	        class="hero"
	        preset="slide-up"
	        trigger="view"
	        duration={360}
	        delay={40}
	        easing="ease-out"
	        distance={24}
	    >
	        Launch the release notes
	    </Motion>
	}`)}
		{DocsCodeBlock("go", `ctx.Motion(server.MotionProps{
	    Tag:      "section",
	    Preset:   server.MotionPresetSlideUp,
	    Trigger:  server.MotionTriggerView,
	    Duration: 360,
	    Delay:    40,
	    Easing:   "ease-out",
	    Distance: 24,
	}, gosx.Text("Launch the release notes"))`)}
		<h2>What the props mean</h2>
		<ul>
			<li>
				<span class="inline-code">as</span>
				or
				<span class="inline-code">tag</span>
				chooses the server-rendered element. The default tag is
				<span class="inline-code">div</span>
				.
			</li>
			<li>
				<span class="inline-code">preset</span>
				selects one of the built-in entrance behaviors:
				<span class="inline-code">fade</span>
				,
				<span class="inline-code">slide-up</span>
				,
				<span class="inline-code">slide-down</span>
				,
				<span class="inline-code">slide-left</span>
				,
				<span class="inline-code">slide-right</span>
				, or
				<span class="inline-code">zoom-in</span>
				.
			</li>
			<li>
				<span class="inline-code">trigger</span>
				is
				<span class="inline-code">load</span>
				or
				<span class="inline-code">view</span>
				.
			</li>
			<li>
				<span class="inline-code">duration</span>
				,
				<span class="inline-code">delay</span>
				,
				<span class="inline-code">easing</span>
				, and
				<span class="inline-code">distance</span>
				control timing and travel.
			</li>
			<li>
				<span class="inline-code">respectReducedMotion</span>
				defaults to true. Set it to false only when the motion is essential feedback instead of decoration.
			</li>
		</ul>
		<section class="callout">
			<strong>What this capability is for</strong>
			<p>
				Use
				<span class="inline-code">Motion</span>
				when you want framework-level entrance animation on normal DOM nodes without pulling the page into a WASM runtime or inventing a custom JS hook per component.
			</p>
		</section>
		<div class="hero-actions">
			<Link class="cta-link" href="/docs/video">Back to video</Link>
			<Link class="cta-link primary" href="/docs/text-layout">Continue to text layout</Link>
		</div>
	</article>
}
