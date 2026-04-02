package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Text Layout</span>
			<p class="lede">
				GoSX can measure and wrap text as a framework primitive instead of leaving layout-sensitive copy to ad hoc browser scripts.
			</p>
		</div>
		<h1>
			<span class="inline-code">TextBlock</span>
			can now stay native to the server when you want measured lines with no JavaScript.
		</h1>
		<p>
			GoSX exposes text layout as a dedicated primitive because line breaking, clamping, and ellipsis are not just styling concerns when the server needs predictable geometry. The
			<span class="inline-code">TextBlock</span>
			helper now supports two explicit modes:
			server-measured
			<span class="inline-code">native</span>
			layout with no bootstrap at all, and the original bootstrap-managed path for pages that still want browser refinement.
		</p>
		<section class="feature-grid">
			<div class="card">
				<strong>native</strong>
				<p>
					The server measures the text, emits final wrapped lines into plain HTML, and keeps the page off the runtime surface.
				</p>
			</div>
			<div class="card">
				<strong>bootstrap</strong>
				<p>
					The server emits a stable first-pass contract and the shared bootstrap runtime can refine it with browser font metrics.
				</p>
			</div>
			<div class="card">
				<strong>Clamp and ellipsis</strong>
				<p>
					Use
					<span class="inline-code">maxLines</span>
					and
					<span class="inline-code">overflow</span>
					to clip or ellipsize long copy with the same API in either mode.
				</p>
			</div>
			<div class="card">
				<strong>Locale-aware inputs</strong>
				<p>
					Font, language, direction, alignment, and white-space all participate in the layout contract instead of living as disconnected CSS guesses.
				</p>
			</div>
		</section>
		<h2>
			Choose the mode explicitly
		</h2>
		<p>
			The default path is still bootstrap so existing pages keep their current behavior. Reach for
			<span class="inline-code">mode="native"</span>
			when you want the wrapped result to come entirely from the server.
		</p>
		{DocsCodeBlock("gosx", `func Page() Node {
    return <TextBlock
        mode="native"
        as="p"
        font="600 18px Iowan Old Style"
        lineHeight={26}
        maxWidth={320}
        maxLines={3}
        overflow="ellipsis"
        lang="en"
        textAlign="center"
        text="A server-measured paragraph that ships as final HTML."
    />
}`)}
		{DocsCodeBlock("go", `ctx.TextBlock(server.TextBlockProps{
    Mode:       server.TextBlockModeNative,
    Tag:        "p",
    Font:       "600 18px Iowan Old Style",
    LineHeight: 26,
    MaxWidth:   320,
    MaxLines:   3,
    Overflow:   textlayout.OverflowEllipsis,
    Align:      "center",
}, gosx.Text("A server-measured paragraph that ships as final HTML."))`)}
		<h2>
			What the props are for
		</h2>
		<ul>
			<li>
				<span class="inline-code">mode</span>
				selects
				<span class="inline-code">bootstrap</span>
				or
				<span class="inline-code">native</span>
				layout.
			</li>
			<li>
				<span class="inline-code">as</span>
				or
				<span class="inline-code">tag</span>
				chooses the server-rendered element. The default tag is
				<span class="inline-code">div</span>
				.
			</li>
			<li>
				<span class="inline-code">text</span>
				or plain-text children provide the visible copy. Use
				<span class="inline-code">source</span>
				when the measured string should differ from the rendered body.
			</li>
			<li>
				<span class="inline-code">font</span>
				,
				<span class="inline-code">lineHeight</span>
				,
				<span class="inline-code">maxWidth</span>
				,
				<span class="inline-code">maxLines</span>
				,
				<span class="inline-code">overflow</span>
				, and
				<span class="inline-code">whiteSpace</span>
				control measurement and wrapping.
			</li>
			<li>
				<span class="inline-code">lang</span>
				,
				<span class="inline-code">dir</span>
				, and
				<span class="inline-code">align</span>
				keep locale and presentation attached to the layout contract.
			</li>
			<li>
				<span class="inline-code">static</span>
				,
				<span class="inline-code">heightHint</span>
				, and
				<span class="inline-code">lineCountHint</span>
				are bootstrap-path controls for observation and first-pass hints.
			</li>
		</ul>
		<section class="callout">
			<strong>Choosing between the two paths</strong>
			<p>
				Use native mode when you want the server to own the final wrapped HTML and keep the page completely JS-free. Use bootstrap mode when exact browser font metrics matter more than staying off the enhancement layer.
			</p>
		</section>
		<div class="hero-actions">
			<Link class="cta-link" href="/docs/motion">Back to motion</Link>
			<Link class="cta-link primary" href="/docs/images">Continue to images</Link>
		</div>
	</article>
}
