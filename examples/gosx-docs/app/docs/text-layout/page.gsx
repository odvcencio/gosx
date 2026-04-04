package docs

func Page() Node {
	return <div>
		<section id="textblock">
			<h2>TextBlock</h2>
			<p>
				<span class="inline-code">server.TextBlock(props, text)</span>
				is GoSX's server-side text measurement primitive.
				It measures the given text against actual font metrics on the server,
				computes line breaks, and emits final wrapped HTML before the first byte
				reaches the browser. No JavaScript, no layout shift, no guessing.
			</p>
			<p>
				No other web framework does this. CSS wrapping happens in the browser
				after fonts load — GoSX resolves it on the server with the same font data
				that would produce the identical result in a canvas context.
			</p>
			{CodeBlock("go", `node := server.TextBlock(server.TextBlockProps{
    Font:     "400 16px Inter",
    MaxWidth: 480,
    MaxLines: 3,
    Overflow: textlayout.OverflowEllipsis,
}, "A long paragraph that the server will measure, wrap, and clip before HTML is sent.")`)}
		</section>

		<section id="font-metrics">
			<h2>Font Metrics</h2>
			<p>
				GoSX ships a font measurement engine that parses TrueType and OpenType metrics
				without CGo. Pass a CSS font shorthand — weight, size, and family — and the
				engine resolves advance widths, kerning pairs, and line height to produce
				character-accurate line breaks.
			</p>
			<p>
				The
				<span class="inline-code">Font</span>
				field accepts the same CSS shorthand string used in
				<span class="inline-code">CanvasRenderingContext2D.font</span>:
			</p>
			{CodeBlock("go", `// CSS shorthand: weight size family
server.TextBlockProps{Font: "700 24px Space Grotesk"}
server.TextBlockProps{Font: "400 14px Inter"}
server.TextBlockProps{Font: "600 18px JetBrains Mono"}`)}
			<p>
				Font files referenced by loaded pages are measured once at startup and cached.
				Subsequent requests pay zero I/O cost for the same font.
			</p>
		</section>

		<section id="width-constraints">
			<h2>Width Constraints</h2>
			<p>
				<span class="inline-code">MaxWidth</span>
				is specified in pixels. The engine breaks lines at word boundaries so that no
				rendered line exceeds that width. Subpixel widths are handled correctly — the
				engine rounds to the nearest pixel boundary rather than truncating.
			</p>
			<p>
				The same text at three different widths produces three different line structures,
				all measured on the server:
			</p>
			{CodeBlock("go", `// 640px: fits on one line
server.TextBlock(server.TextBlockProps{Font: "400 16px Inter", MaxWidth: 640},
    "GoSX measures text server-side with actual font metrics.")

// 320px: breaks into two lines
server.TextBlock(server.TextBlockProps{Font: "400 16px Inter", MaxWidth: 320},
    "GoSX measures text server-side with actual font metrics.")

// 160px: breaks into four lines
server.TextBlock(server.TextBlockProps{Font: "400 16px Inter", MaxWidth: 160},
    "GoSX measures text server-side with actual font metrics.")`)}
			<p>
				When
				<span class="inline-code">MaxWidth</span>
				is zero, the text renders as a single unwrapped line, matching
				browser behavior for elements without a width constraint.
			</p>
		</section>

		<section id="ellipsis-clamping">
			<h2>Ellipsis &amp; Clamping</h2>
			<p>
				<span class="inline-code">MaxLines</span>
				caps the number of rendered lines. Set
				<span class="inline-code">Overflow</span>
				to control what happens when the content exceeds that cap:
			</p>
			<ul>
				<li>
					<span class="inline-code">textlayout.OverflowClip</span>
					— truncates at the last full word that fits.
				</li>
				<li>
					<span class="inline-code">textlayout.OverflowEllipsis</span>
					— appends
					<span class="inline-code">…</span>
					at the measured point where the text would overflow.
				</li>
			</ul>
			{CodeBlock("go", `server.TextBlock(server.TextBlockProps{
    Font:     "400 16px Inter",
    MaxWidth: 320,
    MaxLines: 2,
    Overflow: textlayout.OverflowEllipsis,
}, "This is a long string that will be measured, wrapped to two lines, and ellipsized at the exact character boundary.")`)}
			<p>
				The ellipsis is placed at a character-accurate boundary, not a CSS approximation.
				The server knows the exact advance width of every character and places the
				<span class="inline-code">…</span>
				glyph so that the combined width still fits within
				<span class="inline-code">MaxWidth</span>.
			</p>
		</section>

		<section id="line-breaking">
			<h2>Line Breaking</h2>
			<p>
				The default break strategy wraps at word boundaries (spaces and soft-wrap
				opportunities). You can control whitespace handling with the
				<span class="inline-code">WhiteSpace</span>
				field:
			</p>
			<ul>
				<li>
					<span class="inline-code">"normal"</span>
					— collapses runs of whitespace and wraps at word boundaries. Default.
				</li>
				<li>
					<span class="inline-code">"pre"</span>
					— preserves whitespace and newlines exactly, no wrapping.
				</li>
				<li>
					<span class="inline-code">"pre-wrap"</span>
					— preserves whitespace and newlines, but still wraps at
					<span class="inline-code">MaxWidth</span>.
				</li>
			</ul>
			{CodeBlock("go", `server.TextBlock(server.TextBlockProps{
    Font:       "400 14px JetBrains Mono",
    MaxWidth:   480,
    WhiteSpace: "pre-wrap",
}, "func hello() {\n    fmt.Println(\"world\")\n}")`)}
		</section>

		<section id="bootstrap-mode">
			<h2>Bootstrap Mode</h2>
			<p>
				By default,
				<span class="inline-code">TextBlock</span>
				operates in
				<span class="inline-code">native</span>
				mode: the server owns the final layout and nothing runs in the browser.
				For cases where exact browser font metrics are required — subpixel hinting,
				variable fonts, or OS-level rendering differences —
				<span class="inline-code">bootstrap</span>
				mode emits a server-first stable pass and schedules a lightweight client
				refinement via the shared bootstrap runtime.
			</p>
			{CodeBlock("go", `// native mode: server owns final layout, zero JS
server.TextBlock(server.TextBlockProps{
    Mode:     server.TextBlockModeNative,
    Font:     "700 20px Space Grotesk",
    MaxWidth: 400,
}, "Server-final text, no enhancement needed.")

// bootstrap mode: server first-pass + client refinement
server.TextBlock(server.TextBlockProps{
    Mode:     server.TextBlockModeBootstrap,
    Font:     "700 20px Space Grotesk",
    MaxWidth: 400,
}, "Client will refine if browser metrics differ.")`)}
			<p>
				<strong>Choosing between the modes:</strong>
				use
				<span class="inline-code">native</span>
				when server-measured layout is sufficient and you want the page to remain
				completely JS-free. Use
				<span class="inline-code">bootstrap</span>
				only when browser-specific font rendering produces a meaningfully different
				result and that difference matters for the design.
			</p>
			<section class="callout">
				<strong>No other framework does this</strong>
				<p>
					Every other server rendering framework delegates text layout to the browser.
					GoSX resolves wrap points, ellipsis positions, and line counts on the server
					using the same font data a browser would use. The HTML you ship is the final
					layout, not a placeholder.
				</p>
			</section>
		</section>
	</div>
}
