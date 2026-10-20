package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Images</span>
			<p class="lede">
				Local raster assets can be resized and served through the built-in optimizer without hand-written srcset plumbing.
			</p>
		</div>
		<h1>
			Image handling now covers the production chores instead of leaving raw files untouched.
		</h1>
		<p>
			Local raster assets in
			<span class="inline-code">public/</span>
			can flow through the built-in optimizer at
			<span class="inline-code">/_gosx/image</span>
			. The companion
			<span class="inline-code">Image</span>
			and
			<span class="inline-code">server.Image(...)</span>
			helper exists so apps do not have to hand-write query strings or
			<span class="inline-code">srcset</span>
			values.
		</p>
		<div class="image-frame">
			<Image
				class="demo-image"
				sizes="(max-width: 980px) 100vw, 48rem"
				alt="Generated paper-and-ink sample artwork"
				{...data.image}
			 />
		</div>
		<section class="feature-grid">
			<div class="card">
				<strong>Responsive widths</strong>
				<p>
					The same source asset can emit multiple widths and let the browser choose the right one.
				</p>
			</div>
			<div class="card">
				<strong>Server-side resize</strong>
				<p>
					PNG, JPEG, and GIF sources can be resized before the bytes hit the client.
				</p>
			</div>
			<div class="card">
				<strong>Safe defaults</strong>
				<p>
					Root-relative local paths optimize. Remote URLs and SVGs fall back to a plain img tag.
				</p>
			</div>
			<div class="card">
				<strong>HTML-first</strong>
				<p>
					The optimizer is still just an HTTP endpoint. No client framework magic is required.
				</p>
			</div>
			<div class="card">
				<strong>Art direction</strong>
				<p>
					Ordered server.ImageSource entries add media-specific crops through native picture markup, with per-crop dimensions and explicit wrapper attributes.
				</p>
			</div>
		</section>
		<pre class="code-block">
			{`server.Image(server.ImageProps{
		    Src:    "/paper-card.png",
		    Alt:    "Generated sample artwork",
		    Width:  960,
		    Height: 624,
		    Widths: []int{320, 640, 960},
		})`}
		</pre>
		<pre class="code-block">
			{`server.Image(server.ImageProps{
		    Src:    "https://images.example.com/hero-wide.jpg",
		    Alt:    "Potter shaping clay",
		    Width:  1600,
		    Height: 900,
		    Sources: []server.ImageSource{{
		        Media:  "(max-width: 600px)",
		        SrcSet: "https://images.example.com/hero-tight.jpg 800w",
		        Width:   800,
		        Height:  1000,
		    }},
		    PictureAttrs: gosx.Attrs(
		        gosx.Attr("class", "responsive-picture"),
		    ),
		})`}
		</pre>
		<pre class="code-block">
			{`<Image
		    src={data.hero}
		    alt="Potter shaping clay"
		    width={1600}
		    height={900}
		    sources={data.sources}
		    pictureAttrs={data.pictureAttrs}
		    class="hero-image"
		/>`}
		</pre>
		<div class="hero-actions">
			<Link class="cta-link" href="/docs/runtime">Back to runtime</Link>
			<Link class="cta-link primary" href="/">Back to overview</Link>
		</div>
	</article>
}
