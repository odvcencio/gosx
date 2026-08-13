package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Local image variants</span>
			<p class="lede">
				GoSX can resize local PNG, JPEG, and GIF assets on request and emit responsive image markup. Remote URLs and SVG files remain ordinary image sources.
			</p>
		</div>
		<h2 id="helper">Image helper</h2>
		<CodeBlock lang="go" source={data.imageSample} />
		<p>
			<span class="inline-code">server.Image</span>
			returns an
			<span class="inline-code">img</span>
			node. It sets alt text, dimensions, and optimizer URLs, defaults to lazy loading and async decoding, and switches to eager/high priority when
			<span class="inline-code">Priority</span>
			is true.
		</p>
		<p>
			Paths pass through the configured asset resolver before optimization. Unsupported sources, including remote URLs and SVG, render as plain images rather than being fetched or decoded by the optimizer.
		</p>
		<div class="demo-well" role="region" aria-label="Live optimized image example">
			<p class="demo-well__label">Live helper output</p>
			{data.liveImage}
			<p>
				Inspect this image request: the page emitted a responsive
				<span class="inline-code">srcset</span>
				and local optimizer URLs from
				<span class="inline-code">server.Image</span>
				. The source is a checked-in PNG rendered by the GoSX native scene harness.
			</p>
		</div>
		<h2 id="responsive">Responsive images</h2>
		<CodeBlock lang="go" source={data.responsiveSample} />
		<p>
			<span class="inline-code">Widths</span>
			produces a
			<span class="inline-code">srcset</span>
			. With
			<span class="inline-code">Responsive</span>
			and no explicit list, GoSX derives widths from the configured width. Responsive mode defaults
			<span class="inline-code">sizes</span>
			to
			<span class="inline-code">100vw</span>
			; set a real layout hint when the image is narrower than the viewport.
		</p>
		<h2 id="formats">Formats and sizing</h2>
		<ul>
			<li>
				The built-in encoder supports PNG, JPEG, and GIF output.
			</li>
			<li>
				<span class="inline-code">Quality</span>
				affects JPEG; the default JPEG quality is 82.
			</li>
			<li>
				One requested dimension preserves aspect ratio and does not upscale beyond the source dimension.
			</li>
			<li>
				Both width and height request that exact rectangle, which can change aspect ratio.
			</li>
		</ul>
		<p>
			A GIF source without an explicit output format is encoded as PNG by the current format selection. Request
			<span class="inline-code">gif</span>
			when GIF output is required. WebP and AVIF encoding are not part of the built-in v0.39 handler.
		</p>
		<h2 id="serving">Serving and direct URLs</h2>
		<CodeBlock lang="go" source={data.urlSample} />
		<p>
			<span class="inline-code">server.ImageURL</span>
			accepts a source path and
			<span class="inline-code">server.ImageTransform</span>
			. Calling
			<span class="inline-code">app.SetPublicDir</span>
			mounts the local image handler for that public directory. Static export leaves image sources unoptimized because no request-time transform service is running.
		</p>
		<h2 id="caching">Caching</h2>
		<p>
			Successful optimizer responses carry
			<span class="inline-code">
				Cache-Control: public, max-age=31536000, immutable
			</span>
			. The URL encodes the source path and transform, not the source file contents. Do not replace a production image at the same path while reusing an already-cached transform URL; version the source path or asset URL when bytes change.
		</p>
	</article>
}
