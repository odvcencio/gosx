package watch

func TransportShell(props any) Node {
	return <section class="transport-shell" aria-label="Playback controls prototype">
		<div class="transport-shell__head">
			<div>
				<span class="eyebrow">{props.Badge}</span>
				<h3>{props.Title}</h3>
			</div>
			<p>{props.Subtitle}</p>
		</div>
		<div class="transport-shell__actions">
			<span class="chip chip--primary">{props.PrimaryAction}</span>
			<span class="chip">{props.SecondaryAction}</span>
			<span class="chip">{props.TertiaryAction}</span>
		</div>
	</section>
}
