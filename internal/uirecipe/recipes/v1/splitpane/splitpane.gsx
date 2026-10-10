package ui

// Orientation is vertical for side-by-side panes, horizontal for stacked panes.
type SplitPaneProps struct {
	ID          string
	Orientation string
	Signal      string
	Initial     string
}

component SplitPane(props: SplitPaneProps) {
	return <div
		id={props.ID}
		class={"gsx-split gsx-split--" + props.Orientation}
		style={"--gsx-split-a: " + props.Initial + "px"}
		data-gosx-bind-style={"--gsx-split-a:" + props.Signal + ":px"}
	>
		{children}
	</div>
}

type SplitHandleProps struct {
	Signal      string
	Label       string
	Min         string
	Max         string
	Value       string
}

component SplitHandleX(props: SplitHandleProps) {
	return <div
		class="gsx-split__handle"
		role="separator"
		tabindex="0"
		aria-orientation="vertical"
		aria-label={props.Label}
		aria-valuemin={props.Min}
		aria-valuemax={props.Max}
		aria-valuenow={props.Value}
		data-gosx-drag={props.Signal}
		data-gosx-drag-axis="x"
		data-gosx-drag-min={props.Min}
		data-gosx-drag-max={props.Max}
		data-gosx-drag-step="8"
	></div>
}

component SplitHandleY(props: SplitHandleProps) {
	return <div
		class="gsx-split__handle"
		role="separator"
		tabindex="0"
		aria-orientation="horizontal"
		aria-label={props.Label}
		aria-valuemin={props.Min}
		aria-valuemax={props.Max}
		aria-valuenow={props.Value}
		data-gosx-drag={props.Signal}
		data-gosx-drag-axis="y"
		data-gosx-drag-min={props.Min}
		data-gosx-drag-max={props.Max}
		data-gosx-drag-step="8"
	></div>
}
