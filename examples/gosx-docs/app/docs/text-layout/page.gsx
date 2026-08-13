package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Measured line planning</span>
			<p class="lede">
				TextBlock can emit browser-refined layout metadata or render an approximate server-only line plan. The server estimator does not parse font files or claim exact browser metrics.
			</p>
		</div>
		<h2 id="textblock">TextBlock</h2>
		<CodeBlock lang="go" source={data.blockSample} />
		<p>
			The default
			<span class="inline-code">TextBlockModeBootstrap</span>
			renders ordinary text plus data attributes for the shared bootstrap. Use the page-state or runtime helper, as above, so the required bootstrap is enabled for the document.
		</p>
		<p>
			<span class="inline-code">Text</span>
			is the source string. The tag defaults to
			<span class="inline-code">div</span>
			. Font, language, direction, alignment, width, line height, line count, overflow, and optional first-pass hints are represented explicitly.
		</p>
		<h2 id="modes">Bootstrap and native modes</h2>
		<p>
			Bootstrap mode leaves readable server HTML in place, then measures with browser font metrics and refines the managed block. Set
			<span class="inline-code">Static</span>
			to disable ongoing observation after that setup.
		</p>
		<CodeBlock lang="go" source={data.nativeSample} />
		<p>
			<span class="inline-code">TextBlockModeNative</span>
			does not require the browser bootstrap. It uses
			<span class="inline-code">textlayout.ApproximateMeasurer</span>
			on the server, joins planned lines with newlines, and renders with
			<span class="inline-code">white-space: pre</span>
			. Treat the result as the configured approximation, not exact font shaping.
		</p>
		<h2 id="measurement">Measurement truth</h2>
		<p>
			The approximate measurer derives a font size from the CSS font string and applies character-class width estimates. The browser measurer delegates batches to the GoSX bootstrap and uses actual browser canvas font measurement. A caching wrapper memoizes widths by font key and token text.
		</p>
		<p>
			<span class="inline-code">EstimateTextBlockMetrics</span>
			returns approximate line count, height, width, byte/rune counts, and truncation state when text and maximum width are available. Those values can seed
			<span class="inline-code">HeightHint</span>
			and
			<span class="inline-code">LineCountHint</span>
			before refinement.
		</p>
		<h2 id="constraints">Width, lines, and overflow</h2>
		<p>
			<span class="inline-code">MaxWidth</span>
			controls line breaking.
			<span class="inline-code">MaxLines</span>
			clamps the output. Choose
			<span class="inline-code">OverflowClip</span>
			or
			<span class="inline-code">OverflowEllipsis</span>
			for a clamped final line. A non-positive line height uses the framework's approximate font-size-based fallback.
		</p>
		<h2 id="whitespace">Whitespace</h2>
		<p>
			Use the typed constants
			<span class="inline-code">textlayout.WhiteSpaceNormal</span>
			,
			<span class="inline-code">WhiteSpacePreWrap</span>
			, or
			<span class="inline-code">WhiteSpacePre</span>
			. Normal mode collapses whitespace; the pre modes preserve explicit structure according to their line-wrapping behavior.
		</p>
		<h2 id="low-level">Low-level pipeline</h2>
		<CodeBlock lang="go" source={data.lowLevelSample} />
		<p>
			<span class="inline-code">Prepare</span>
			normalizes and tokenizes once,
			<span class="inline-code">Measure</span>
			attaches batched widths, and
			<span class="inline-code">Layout</span>
			computes line ranges and metrics. Reuse prepared or measured values when laying out the same text under multiple constraints.
		</p>
	</article>
}
