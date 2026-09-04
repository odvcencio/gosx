package ui

// CardProps describes the card's editorial hierarchy.
// Variant: default, raised, or quiet.
type CardProps struct {
	Variant     string
	Eyebrow     string
	Title       string
	Description string
}

// Card renders a self-contained article and projects caller-owned body content.
component Card(props: CardProps) {
	return <article class={"gsx-card gsx-card--" + props.Variant}>
		<header class="gsx-card__header">
			<p class="gsx-card__eyebrow">{props.Eyebrow}</p>
			<h3 class="gsx-card__title">{props.Title}</h3>
			<p class="gsx-card__description">{props.Description}</p>
		</header>
		<div class="gsx-card__content">{children}</div>
	</article>
}
