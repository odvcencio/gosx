package watch

func HighlightCard(props any) Node {
	return <section class="feature-card">
		<span class="eyebrow">{props.Kicker}</span>
		<h3>{props.Title}</h3>
		<p>{props.Body}</p>
	</section>
}

func RoomPreview(props any) Node {
	return <article class="room-preview">
		<div class="room-preview__top">
			<span class="eyebrow">{props.Status}</span>
			<span class="mono">{props.SubtitleState}</span>
		</div>
		<strong>{props.DisplayTitle}</strong>
		<p>{props.Title}</p>
	</article>
}

func Page() Node {
	return <article class="landing">
		<section class="hero">
			<div class="hero__copy">
				<span class="eyebrow">{data.hero.eyebrow}</span>
				<h1>{data.hero.title}</h1>
				<p class="lede">{data.hero.lede}</p>
				<div class="hero__actions">
					<Link href={data.hero.watch_href} class="button button--primary">Open prototype room</Link>
					<a href="#rooms" data-gosx-link class="button">Inspect the normalized queue</a>
				</div>
				<p class="hero__note">{data.hero.note}</p>
			</div>
			<aside class="hero__aside">
				<div class="status-card">
					<span class="eyebrow">Architecture</span>
					<p>Server renders the room, queue, and subtitle facts before the browser touches playback.</p>
					<div class="status-card__meta">
						<span>Document-first</span>
						<span>Transport-later</span>
						<span>Normalized titles</span>
					</div>
				</div>
			</aside>
		</section>
		<section class="highlights">
			<Each as="feature" of={data.highlights}>
				<HighlightCard {...feature}></HighlightCard>
			</Each>
		</section>
		<section class="room-grid" id="rooms">
			<div class="room-grid__intro">
				<span class="eyebrow">Queued rooms</span>
				<h2>Render the queue with titles the server already normalized.</h2>
			</div>
			<div class="room-grid__list">
				<Each as="room" of={data.rooms}>
					<RoomPreview {...room}></RoomPreview>
				</Each>
			</div>
		</section>
	</article>
}
