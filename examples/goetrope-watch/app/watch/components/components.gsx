package components

type QueueRowProps struct {
	Index         string
	DisplayTitle  string
	Year          string
	SubtitleState string
}

component QueueRow(props: QueueRowProps) {
	return <div class="queue-row">
		<span class="queue-row__index">{props.Index}</span>
		<div class="queue-row__body">
			<strong>{props.DisplayTitle}</strong>
			<p>{props.Year}</p>
		</div>
		<span class="queue-row__state">{props.SubtitleState}</span>
	</div>
}

type MomentProps struct {
	Label string
	Value string
}

component Moment(props: MomentProps) {
	return <div class="moment">
		<span class="eyebrow">{props.Label}</span>
		<p>{props.Value}</p>
	</div>
}

type TransportShellProps struct {
	Badge           string
	Title           string
	Subtitle        string
	PrimaryAction   string
	SecondaryAction string
	TertiaryAction  string
}

component TransportShell(props: TransportShellProps) {
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
