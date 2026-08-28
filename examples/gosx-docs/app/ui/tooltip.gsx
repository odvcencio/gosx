package ui

// TooltipProps is an accessible tooltip's typed props: the id that
// associates the trigger with its tooltip text (aria-describedby), and the
// tooltip's own text content.
//
// ID is caller-supplied rather than generated here. The Go version this
// component replaced generated it from a package-level atomic counter, and
// a strict component cannot reproduce that: its body may render literals,
// concatenate them, and select props fields, but it may not call an
// arbitrary Go function or hold state (see ir's strict-server-expression
// hint, "use literals or props field selection; compute, index, and call
// methods before rendering") — a stateful id counter has to run in the
// caller.
type TooltipProps struct {
	ID      string
	Content string
}

// Tooltip renders a trigger element with an accessible tooltip overlay.
// The trigger is the caller's own markup, passed as children — see the
// shared components design's children contract: children arrive already
// rendered, in the caller's scope, so this component never needs to know
// what the trigger looks like, only where the tooltip text attaches to it.
component Tooltip(props: TooltipProps) {
	return <span class="tooltip-trigger" aria-describedby={props.ID}>
		{children}
		<span id={props.ID} class="tooltip glass-panel" role="tooltip">{props.Content}</span>
	</span>
}
