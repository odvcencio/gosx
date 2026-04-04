package docs

func Page() Node {
	return <div>
		<section id="image-optimization">
			<h2>Image Optimization</h2>
			<p>
				Local raster assets in
				<span class="inline-code">public/</span>
				can be served through the built-in optimizer at
				<span class="inline-code">/_gosx/image</span>
				. Pass
				<span class="inline-code">src</span>
				,
				<span class="inline-code">w</span>
				,
				<span class="inline-code">h</span>
				, and
				<span class="inline-code">fmt</span>
				as query parameters to request a resized and converted variant.
			</p>
			{CodeBlock("html", `<img
	    src="/_gosx/image?src=/hero.png&w=960&h=540&fmt=webp"
	    width="960"
	    height="540"
	    alt="Hero image"
	/>`)}
			<p>
				The
				<span class="inline-code">server.Image</span>
				helper builds these URLs and generates a complete
				<span class="inline-code">img</span>
				element with
				<span class="inline-code">srcset</span>
				,
				<span class="inline-code">sizes</span>
				,
				<span class="inline-code">width</span>
				, and
				<span class="inline-code">height</span>
				attributes so apps do not have to assemble query strings by hand.
			</p>
			{CodeBlock("go", `node := server.Image(server.ImageProps{
	    Src:    "/hero.png",
	    Alt:    "Hero image",
	    Width:  960,
	    Height: 540,
	    Widths: []int{320, 640, 960},
	})`)}
		</section>
		<section id="responsive-images">
			<h2>Responsive Images</h2>
			<p>
				The
				<span class="inline-code">Widths</span>
				field on
				<span class="inline-code">server.ImageProps</span>
				produces a
				<span class="inline-code">srcset</span>
				attribute with one entry per requested width. The browser selects the most appropriate variant based on the current viewport and the provided
				<span class="inline-code">sizes</span>
				hint.
			</p>
			{CodeBlock("go", `server.Image(server.ImageProps{
	    Src:    "/photo.jpg",
	    Alt:    "Landscape photo",
	    Width:  1280,
	    Height: 720,
	    Widths: []int{400, 800, 1280},
	    Sizes:  "(max-width: 768px) 100vw, 50vw",
	})`)}
			<p>
				The generated
				<span class="inline-code">srcset</span>
				references the
				<span class="inline-code">/_gosx/image</span>
				endpoint for each width, so the original full-size asset is never sent to clients that do not need it.
			</p>
			{CodeBlock("html", `<img
	    src="/_gosx/image?src=/photo.jpg&w=1280&h=720"
	    srcset="
	        /_gosx/image?src=/photo.jpg&w=400  400w,
	        /_gosx/image?src=/photo.jpg&w=800  800w,
	        /_gosx/image?src=/photo.jpg&w=1280 1280w"
	    sizes="(max-width: 768px) 100vw, 50vw"
	    width="1280"
	    height="720"
	    alt="Landscape photo"
	/>`)}
		</section>
		<section id="format-conversion">
			<h2>Format Conversion</h2>
			<p>
				The
				<span class="inline-code">fmt</span>
				query parameter requests a specific output format. PNG, JPEG, and GIF sources are supported as inputs. WebP is the recommended output format for browsers that support it.
			</p>
			{CodeBlock("go", `// Request a WebP variant at a specific width
	url := server.ImageURL(server.ImageURLProps{
	    Src:    "/banner.png",
	    Width:  800,
	    Format: "webp",
	})`)}
			<p>
				Remote URLs and SVG sources are passed through unchanged. The optimizer only processes root-relative paths that resolve to files in the
				<span class="inline-code">public/</span>
				directory.
			</p>
		</section>
		<section id="caching">
			<h2>Caching</h2>
			<p>
				Optimized images are served with
				<span class="inline-code">
					Cache-Control: public, immutable, max-age=31536000
				</span>
				by default. The URL includes the source path, dimensions, and format, so cache keys are content-addressed: a change to the source file at a new path produces a new URL with no stale responses.
			</p>
			<p>
				These URLs are CDN-friendly. When a CDN sits in front of the origin, the first request for each variant is forwarded and cached at the edge. Subsequent requests are served from cache without hitting the Go server.
			</p>
			{CodeBlock("http", `HTTP/1.1 200 OK
	Content-Type: image/webp
	Cache-Control: public, immutable, max-age=31536000
	Vary: Accept`)}
			<div class="callout">
				<strong>Development mode</strong>
				<p>
					In development the immutable header is omitted so refreshing the browser always fetches a fresh variant while you iterate on source assets.
				</p>
			</div>
		</section>
	</div>
}
