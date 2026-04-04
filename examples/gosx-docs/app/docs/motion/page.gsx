package docs

func Page() Node {
	return <div>
		<section id="motion-presets">
			<h2>Motion Presets</h2>
			<p>
				GoSX exposes entrance animation as a server-authored primitive. Use
				<span class="inline-code">&lt;Motion /&gt;</span>
				in a
				<span class="inline-code">.gsx</span>
				template or
				<span class="inline-code">server.Motion()</span>
				in Go to apply a named preset. The element renders as ordinary HTML on the
				server first, then the shared bootstrap runtime upgrades it into a managed
				entrance animation when JavaScript is available.
			</p>
			<p>
				Available presets:
			</p>
			<ul>
				<li><span class="inline-code">fade</span> — opacity 0 to 1.</li>
				<li><span class="inline-code">slide-up</span> — translates from below and fades in.</li>
				<li><span class="inline-code">slide-down</span> — translates from above and fades in.</li>
				<li><span class="inline-code">slide-left</span> — translates from the right and fades in.</li>
				<li><span class="inline-code">slide-right</span> — translates from the left and fades in.</li>
				<li><span class="inline-code">zoom-in</span> — scales from slightly below 1 and fades in.</li>
			</ul>
			{CodeBlock("gosx", `func Page() Node {
    return <Motion as="section" preset="slide-up" trigger="view">
        <h2>Section heading</h2>
        <p>This content slides into view when the user scrolls to it.</p>
    </Motion>
}`)}
			{CodeBlock("go", `server.Motion(server.MotionProps{
    Tag:    "section",
    Preset: server.MotionPresetSlideUp,
    Trigger: server.MotionTriggerView,
}, gosx.El("h2", gosx.Text("Section heading")),
   gosx.El("p", gosx.Text("This content slides into view when the user scrolls to it.")))`)}
			<p>
				Both authoring paths produce identical HTML output. The
				<span class="inline-code">.gsx</span>
				path is ergonomic for page authors; the Go path is useful when motion
				props are computed or conditional.
			</p>
		</section>

		<section id="viewport-triggers">
			<h2>Viewport Triggers</h2>
			<p>
				The
				<span class="inline-code">trigger</span>
				prop controls when the animation fires:
			</p>
			<ul>
				<li>
					<span class="inline-code">load</span>
					— runs immediately when the page bootstrap executes, suitable for
					hero elements and above-the-fold content.
				</li>
				<li>
					<span class="inline-code">view</span>
					— defers the animation until the element enters the viewport via
					<span class="inline-code">IntersectionObserver</span>, suitable for
					content that appears as the user scrolls.
				</li>
			</ul>
			{CodeBlock("gosx", `<Motion preset="fade" trigger="load">
    Above the fold, fades in on page entry.
</Motion>

<Motion preset="slide-up" trigger="view">
    Below the fold, animates when scrolled into view.
</Motion>`)}
			<p>
				Elements with
				<span class="inline-code">trigger="view"</span>
				are held invisible until the observer fires. If JavaScript is unavailable,
				the element renders as visible plain HTML with no hidden state. The motion
				is enhancement, not a rendering gate.
			</p>
		</section>

		<section id="reduced-motion">
			<h2>Reduced Motion</h2>
			<p>
				The bootstrap runtime checks
				<span class="inline-code">prefers-reduced-motion: reduce</span>
				before applying any animation. When the media query matches, all Motion
				elements are resolved immediately without animating — the content becomes
				visible instantly.
			</p>
			<p>
				This behavior is on by default. You can opt a specific element out of
				the policy only when the motion is essential feedback rather than decoration:
			</p>
			{CodeBlock("gosx", `<Motion preset="fade" trigger="load" respectReducedMotion={true}>
    Respects the user's reduced-motion preference. Default.
</Motion>

<Motion preset="fade" trigger="load" respectReducedMotion={false}>
    Always animates, even if the user prefers reduced motion.
    Only use for motion that communicates state, not aesthetics.
</Motion>`)}
			<section class="callout">
				<strong>Accessibility policy</strong>
				<p>
					WCAG 2.1 Success Criterion 2.3.3 (AAA) recommends honoring
					<span class="inline-code">prefers-reduced-motion</span>. GoSX makes
					the accessible path the default. Setting
					<span class="inline-code">respectReducedMotion={false}</span>
					is an explicit opt-out that should be reserved for progress indicators
					or other motion that carries meaning.
				</p>
			</section>
		</section>

		<section id="custom-timing">
			<h2>Custom Timing</h2>
			<p>
				All timing parameters are optional. Omitting them falls back to
				sensible preset defaults. Override when the design requires precise control:
			</p>
			<ul>
				<li>
					<span class="inline-code">duration</span>
					— animation duration in milliseconds. Defaults vary by preset (200–400ms).
				</li>
				<li>
					<span class="inline-code">delay</span>
					— delay before the animation starts, in milliseconds. Useful for
					staggering sibling elements.
				</li>
				<li>
					<span class="inline-code">easing</span>
					— a CSS easing function string:
					<span class="inline-code">ease</span>,
					<span class="inline-code">ease-out</span>,
					<span class="inline-code">linear</span>, or any
					<span class="inline-code">cubic-bezier(...)</span> value.
				</li>
				<li>
					<span class="inline-code">distance</span>
					— translation distance in pixels for slide presets. Defaults to 24px.
				</li>
			</ul>
			{CodeBlock("gosx", `<Motion
    preset="slide-up"
    trigger="view"
    duration={360}
    delay={80}
    easing="ease-out"
    distance={32}
>
    Custom timing applied to this element.
</Motion>`)}
			{CodeBlock("go", `server.Motion(server.MotionProps{
    Tag:      "div",
    Preset:   server.MotionPresetSlideUp,
    Trigger:  server.MotionTriggerView,
    Duration: 360,
    Delay:    80,
    Easing:   "ease-out",
    Distance: 32,
}, content)`)}
			<p>
				Stagger a group of sibling cards by incrementing
				<span class="inline-code">delay</span>
				by a fixed step (40–80ms is a common value) on each item.
				The viewport trigger means they will begin their stagger only when
				the section scrolls into view, not immediately on page load.
			</p>
			{CodeBlock("go", `for i, card := range cards {
    nodes = append(nodes, server.Motion(server.MotionProps{
        Tag:     "article",
        Preset:  server.MotionPresetSlideUp,
        Trigger: server.MotionTriggerView,
        Delay:   i * 60,
    }, renderCard(card)))
}`)}
		</section>
	</div>
}
