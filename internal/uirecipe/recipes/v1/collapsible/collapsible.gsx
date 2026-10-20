package ui

type CollapsibleProps struct {
	Signal  string
	Summary string
	Open    bool
}

component Collapsible(props: CollapsibleProps) {
	return <details class="gsx-collapsible" data-gosx-collapsible={props.Signal} open={props.Open}>
		<summary>{props.Summary}</summary>
		{children}
	</details>
}
