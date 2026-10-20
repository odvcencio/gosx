package ui

// CapabilityTagProps is a small tag badge's typed props.
type CapabilityTagProps struct {
	Label string
}

// CapabilityTag renders a small tag badge. It replaces the hand-built
// gosx.El version in app/components.go; app.CapabilityTag now renders this
// component through route.RenderProgramComponent, so every existing
// {CapabilityTag(label)} call site keeps working unchanged.
component CapabilityTag(props: CapabilityTagProps) {
	return <span class="cap-tag">{props.Label}</span>
}
