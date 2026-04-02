package docs

func Page() Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Video</span>
			<p class="lede">
				GoSX now treats video as a framework primitive instead of a hand-managed pile of
				<span class="inline-code">&lt;video&gt;</span>
				attributes and bootstrap glue.
			</p>
		</div>
		<h1>
			<span class="inline-code">Video</span>
			now ships a real server baseline first, then upgrades into managed playback only when the page needs it.
		</h1>
		<p>
			The
			<span class="inline-code">&lt;Video /&gt;</span>
			builtin,
			<span class="inline-code">server.Video(...)</span>
			, and
			<span class="inline-code">ctx.Video(...)</span>
			all render a plain
			<span class="inline-code">&lt;video&gt;</span>
			element with authored
			<span class="inline-code">&lt;source&gt;</span>
			and
			<span class="inline-code">&lt;track&gt;</span>
			children. When the built-in video engine mounts, it upgrades that baseline in place so HLS fallback, subtitle loading, sync, and shared media signals do not require a separate authoring model.
		</p>
		<section class="feature-grid">
			<div class="card">
				<strong>HTML-first baseline</strong>
				<p>
					Server output is a usable
					<span class="inline-code">&lt;video&gt;</span>
					element, not an empty div waiting for client JS to fabricate media markup.
				</p>
			</div>
			<div class="card">
				<strong>Multiple sources</strong>
				<p>
					Provide
					<span class="inline-code">src</span>
					for a single transport or
					<span class="inline-code">sources</span>
					for an authored source list. The runtime can still step in for HLS when native playback is unavailable.
				</p>
			</div>
			<div class="card">
				<strong>Tracks stay declarative</strong>
				<p>
					Subtitle tracks can come from
					<span class="inline-code">subtitleBase</span>
					or explicit per-track
					<span class="inline-code">src</span>
					values, and the same declaration feeds both server markup and runtime loading.
				</p>
			</div>
			<div class="card">
				<strong>Shared media signals</strong>
				<p>
					The built-in engine keeps publishing position, duration, subtitles, mute state, and transport errors through the shared
					<span class="inline-code">$video.*</span>
					signal surface.
				</p>
			</div>
		</section>
		<h2>
			Author the same contract in
			<span class="inline-code">.gsx</span>
			or Go
		</h2>
		{DocsCodeBlock("gosx", `func Page() Node {
	    return <Video
	        poster="/media/poster.jpg"
	        controls
	        muted
	        playsInline
	        subtitleTrack="en"
	        sources={[
	            {"src": "/media/promo.webm", "type": "video/webm"},
	            {"src": "/media/promo.m3u8", "type": "application/vnd.apple.mpegurl"},
	        ]}
	        subtitleTracks={[
	            {"id": "en", "language": "en", "title": "English", "src": "/subs/en.vtt"},
	        ]}
	    >
	        <p>Download the trailer</p>
	    </Video>
	}`)}
		{DocsCodeBlock("go", `ctx.Video(server.VideoProps{
	    Poster:   "/media/poster.jpg",
	    Controls: true,
	    Muted:    true,
	    PlaysInline: true,
	    Sources: []server.VideoSource{
	        {Src: "/media/promo.webm", Type: "video/webm"},
	        {Src: "/media/promo.m3u8", Type: "application/vnd.apple.mpegurl"},
	    },
	    SubtitleTrack: "en",
	    SubtitleTracks: []server.VideoTrack{
	        {ID: "en", Language: "en", Title: "English", Src: "/subs/en.vtt"},
	    },
	    Sync: "wss://watch.example.com/party/demo",
	}, gosx.El("p", gosx.Text("Download the trailer")))`)}
		<h2>What the props mean</h2>
		<ul>
			<li>
				<span class="inline-code">src</span>
				or
				<span class="inline-code">sources</span>
				choose the transport inputs. Use
				<span class="inline-code">sources</span>
				when you want authored
				<span class="inline-code">&lt;source&gt;</span>
				children.
			</li>
			<li>
				<span class="inline-code">poster</span>
				,
				<span class="inline-code">preload</span>
				,
				<span class="inline-code">crossOrigin</span>
				,
				<span class="inline-code">width</span>
				, and
				<span class="inline-code">height</span>
				control the baseline media element.
			</li>
			<li>
				<span class="inline-code">autoPlay</span>
				,
				<span class="inline-code">controls</span>
				,
				<span class="inline-code">loop</span>
				,
				<span class="inline-code">muted</span>
				,
				<span class="inline-code">playsInline</span>
				,
				<span class="inline-code">volume</span>
				, and
				<span class="inline-code">rate</span>
				shape playback defaults and runtime-controlled state.
			</li>
			<li>
				<span class="inline-code">subtitleTrack</span>
				,
				<span class="inline-code">subtitleTracks</span>
				, and
				<span class="inline-code">subtitleBase</span>
				declare the text-track contract once for both server markup and runtime loading.
			</li>
			<li>
				<span class="inline-code">sync</span>
				,
				<span class="inline-code">syncMode</span>
				, and
				<span class="inline-code">syncStrategy</span>
				opt the player into watch-party style lead/follow transport.
			</li>
			<li>
				<span class="inline-code">hls</span>
				and
				<span class="inline-code">hlsConfig</span>
				let the engine tune HLS.js when native HLS playback is not available.
			</li>
		</ul>
		<section class="callout">
			<strong>What changed materially</strong>
			<p>
				GoSX video is no longer “JS owns the player, HTML is just fallback copy.” The HTML baseline is now the real player shell, and the runtime adopts it in place only when the richer transport and signal features are needed.
			</p>
		</section>
		<div class="hero-actions">
			<Link class="cta-link" href="/docs/engines">Back to engines</Link>
			<Link class="cta-link primary" href="/docs/motion">Continue to motion</Link>
		</div>
	</article>
}
