package ui

// StatCardProps is a proof-point stat card's typed props: a headline value
// and the label under it.
type StatCardProps struct {
	Value string
	Label string
}

// StatCard renders a proof-point stat card. It replaces the hand-built
// gosx.El version that used to live in app/components.go; every call site
// moved from {StatCard(value, label)} to <ui.StatCard Value={value}
// Label={label}/>.
component StatCard(props: StatCardProps) {
	return <div class="stat-card glass-panel">
		<span class="stat-card__value">{props.Value}</span>
		<span class="stat-card__label">{props.Label}</span>
	</div>
}
