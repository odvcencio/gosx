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
			node by default, or a native
			<span class="inline-code">picture</span>
			when art-direction sources are present. It sets alt text, dimensions, and optimizer URLs, defaults to lazy loading and async decoding, and switches to eager/high priority when
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
		<h2 id="builtin">The &lt;Image&gt; builtin</h2>
		<p>
			<span class="inline-code">&lt;Image&gt;</span>
			is the JSX tag form of the same helper, checked at build time instead of only at render time. Every
			<span class="inline-code">&lt;Image&gt;</span>
			requires a non-empty
			<span class="inline-code">alt</span>
			.
		</p>
		<CodeBlock lang="gsx" source={data.builtinLocalSample} />
		<p>
			A local source (a root-relative path such as
			<span class="inline-code">/photos/harbor.jpg</span>
			) needs no
			<span class="inline-code">width</span>
			or
			<span class="inline-code">height</span>
			:
			<span class="inline-code">gosx check</span>
			reads the file under
			<span class="inline-code">public/</span>
			and the renderer injects its intrinsic dimensions automatically. A local source naming no file under
			<span class="inline-code">public/</span>
			fails
			<span class="inline-code">gosx check</span>
			.
		</p>
		<CodeBlock lang="gsx" source={data.builtinExternalSample} />
		<p>
			An external source (an
			<span class="inline-code">http://</span>
			or
			<span class="inline-code">https://</span>
			URL) is never proxied or resized this release — it renders exactly as given. Because its dimensions cannot be probed at build time, an external (or otherwise dynamic) source requires an explicit
			<span class="inline-code">width</span>
			and
			<span class="inline-code">height</span>
			; omitting either fails
			<span class="inline-code">gosx check</span>
			too.
		</p>
		<p>
			When
			<span class="inline-code">gosx build</span>
			has generated responsive variants for a local source (see
			<a href="#formats">Formats &amp; Sizing</a>
			), the rendered
			<span class="inline-code">&lt;Image&gt;</span>
			becomes a plain
			<span class="inline-code">&lt;img&gt;</span>
			with a real srcset in the source's own format — GoSX ships no WebP encoder, so a JPEG source keeps a JPEG ladder and a PNG source keeps a PNG ladder. A project that registers its own WebP
			<span class="inline-code">imagepipe.Encoder</span>
			gets a
			<span class="inline-code">&lt;picture&gt;</span>
			element instead: a WebP
			<span class="inline-code">&lt;source&gt;</span>
			plus that same native-format
			<span class="inline-code">&lt;img&gt;</span>
			fallback. Without a prior build — for example during
			<span class="inline-code">gosx dev</span>
			— it falls back to the request-time optimizer described above, so a page under active development always renders.
		</p>
		<p>
			<span class="inline-code">&lt;Image&gt;</span>
			is not supported inside an island component: an island re-renders on the client from its own program and cannot rebuild this server-rendered markup. Use a plain
			<span class="inline-code">&lt;img&gt;</span>
			element inside an island instead, with
			<span class="inline-code">width</span>
			and
			<span class="inline-code">height</span>
			set explicitly.
		</p>
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
		<h2 id="art-direction">Art direction</h2>
		<CodeBlock lang="go" source={data.artDirectionSample} />
		<CodeBlock lang="go" source={data.builtinArtDirectionSample} />
		<p>
			Use ordered
			<span class="inline-code">server.ImageSource</span>
			entries when a layout needs a different crop at a media breakpoint, not merely a smaller copy of the same composition. Supplying at least one non-empty
			<span class="inline-code">SrcSet</span>
			wraps the fallback image in
			<span class="inline-code">&lt;picture&gt;</span>
			and emits the authored sources first, in slice order. The
			<span class="inline-code">&lt;Image&gt;</span>
			builtin accepts the same contract through
			<span class="inline-code">sources</span>
			; legacy route data may use a slice of maps with
			<span class="inline-code">srcset</span>
			,
			<span class="inline-code">media</span>
			,
			<span class="inline-code">sizes</span>
			,
			<span class="inline-code">type</span>
			,
			<span class="inline-code">width</span>
			, and
			<span class="inline-code">height</span>
			keys.
		</p>
		<p>
			Set positive source
			<span class="inline-code">Width</span>
			and
			<span class="inline-code">Height</span>
			when a crop has a different aspect ratio from the fallback image. Apply layout attributes to the wrapper through
			<span class="inline-code">PictureAttrs: gosx.Attrs(...)</span>
			in Go, or
			<span class="inline-code">pictureAttrs</span>
			in a file route. Route data may provide either a
			<span class="inline-code">gosx.AttrList</span>
			or a string-keyed
			<span class="inline-code">map[string]any</span>
			with a
			<span class="inline-code">class</span>
			key for
			<span class="inline-code">responsive-picture</span>
			; map keys render in deterministic order. Ordinary component attributes such as
			<span class="inline-code">class="hero-image"</span>
			stay on the fallback image. Picture attributes do nothing when no wrapper is emitted. Map-provided names are validated, and names and values render through GoSX's normal escaping path.
		</p>
		<p>
			Source candidate strings are escaped into markup but otherwise remain author-owned. GoSX does not download, cache, or rewrite remote candidates, and this feature adds no browser JavaScript.
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
			when GIF output is required. WebP and AVIF encoding are not part of the built-in
			<span class="inline-code">gosx</span>
			handler at all: GoSX ships no WebP or AVIF encoder (no wasm runtime, no FFI shim). The build-time
			<span class="inline-code">imagepipe.RegisterEncoder</span>
			extension point adds either back for a project willing to take on that encoder's own dependency itself.
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
