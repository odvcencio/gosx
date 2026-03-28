package watch

func QueueRow(props any) Node {
	return <div class="queue-row">
		<span class="queue-row__index">{props.Index}</span>
		<div class="queue-row__body">
			<strong>{props.DisplayTitle}</strong>
			<p>{props.Year}</p>
		</div>
		<span class="queue-row__state">{props.SubtitleState}</span>
	</div>
}

func Moment(props any) Node {
	return <div class="moment">
		<span class="eyebrow">{props.Label}</span>
		<p>{props.Value}</p>
	</div>
}

func Page() Node {
	return <article class="watch-page">
		<section class="watch-hero">
			<div class="watch-hero__copy">
				<span class="eyebrow">{data.room.live_state}</span>
				<h1>{data.current_item.display_title}</h1>
				<p class="lede">{data.current_item.synopsis}</p>
				<div class="watch-hero__meta">
					<span>{data.current_item.year}</span>
					<span>{data.current_item.runtime}</span>
					<span>{data.stream.latency}</span>
					<span>{data.room.viewer_count} viewers</span>
				</div>
			</div>
			<div class="watch-hero__rail">
				<div class="rail-card rail-card--strong">
					<span class="eyebrow">Canonical room</span>
					<strong>{data.room.title}</strong>
					<p>{data.room.tagline}</p>
				</div>
				<div class="rail-card">
					<span class="eyebrow">Stream</span>
					<p>{data.stream.status} · {data.stream.quality} · {data.stream.buffer}</p>
				</div>
			</div>
		</section>
		<section class="watch-grid">
			<div class="watch-grid__main">
				<div class="screen-card">
					<div class="screen-card__topline">
						<span class="eyebrow">{data.subtitle_state.label}</span>
						<span class="mono">{data.subtitle_state.mode}</span>
					</div>
					<h2>Server-renders the room first. Transport stays narrow.</h2>
					<p>{data.subtitle_state.detail}</p>
					<TransportShell {...data.player}></TransportShell>
				</div>
			</div>
			<aside class="watch-grid__side">
				<section class="stack-card">
					<div class="stack-card__head">
						<span class="eyebrow">Queue</span>
						<span class="mono">{data.subtitle_state.track}</span>
					</div>
					<div class="queue-list">
						<Each as="item" of={data.queue}>
							<QueueRow {...item}></QueueRow>
						</Each>
					</div>
				</section>
				<section class="stack-card">
					<span class="eyebrow">Subtitle cache</span>
					<h3>{data.subtitle_state.label}</h3>
					<p>{data.subtitle_state.queued}</p>
					<p class="stack-card__note">{data.subtitle_state.detail}</p>
				</section>
				<section class="stack-card">
					<span class="eyebrow">Next up</span>
					<h3>{data.next_up.display_title}</h3>
					<p>{data.next_up.reason}</p>
				</section>
				<section class="stack-card">
					<span class="eyebrow">Moments</span>
					<div class="moment-list">
						<Each as="moment" of={data.moments}>
							<Moment {...moment}></Moment>
						</Each>
					</div>
				</section>
			</aside>
		</section>
	</article>
}
