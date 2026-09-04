package ui

// ButtonProps keeps every visual and native state explicit at the call site.
// Variant: primary, secondary, ghost, or danger. Size: sm, md, or lg.
type ButtonProps struct {
	Type     string
	Variant  string
	Size     string
	Disabled bool
}

// Button renders a native button and projects caller-owned label or icon markup.
component Button(props: ButtonProps) {
	return <button
		class={"gsx-button gsx-button--" + props.Variant + " gsx-button--" + props.Size}
		type={props.Type}
		disabled={props.Disabled}
	>
		{children}
	</button>
}
