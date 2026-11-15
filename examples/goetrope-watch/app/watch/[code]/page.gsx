package watch

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
					<span>
						{data.room.viewer_count}
						viewers
					</span>
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
					<p>
						{data.stream.status}
						·
						{data.stream.quality}
						·
						{data.stream.buffer}
					</p>
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
					<h2>
						Server-renders the room first. Transport stays narrow.
					</h2>
					<p>{data.subtitle_state.detail}</p>
					<section class="transport-shell" aria-label="Playback controls prototype">
						<div class="transport-shell__head">
							<div>
								<span class="eyebrow">{data.player.badge}</span>
								<h3>{data.player.title}</h3>
							</div>
							<p>{data.player.subtitle}</p>
						</div>
						<div class="transport-shell__actions">
							<span class="chip chip--primary">{data.player.primary_action}</span>
							<span class="chip">{data.player.secondary_action}</span>
							<span class="chip">{data.player.tertiary_action}</span>
						</div>
					</section>
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
							<div class="queue-row">
								<span class="queue-row__index">{item.index}</span>
								<div class="queue-row__body">
									<strong>{item.display_title}</strong>
									<p>{item.year}</p>
								</div>
								<span class="queue-row__state">{item.subtitle_state}</span>
							</div>
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
							<div class="moment">
								<span class="eyebrow">{moment.label}</span>
								<p>{moment.value}</p>
							</div>
						</Each>
					</div>
				</section>
			</aside>
		</section>
	</article>
}
