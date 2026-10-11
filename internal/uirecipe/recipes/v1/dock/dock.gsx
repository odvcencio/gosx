package ui

type DockProps struct {
	ID           string
	LeftSignal   string
	RightSignal  string
	BottomSignal string
	Left         string
	Right        string
	Bottom       string
}

// Children use the gsx-dock__top, __left, __center, __right and __bottom classes.
component Dock(props: DockProps) {
	return <div
		id={props.ID}
		class="gsx-dock"
		style={"--gsx-dock-left: " + props.Left + "px; --gsx-dock-right: " + props.Right + "px; --gsx-dock-bottom: " + props.Bottom + "px"}
		data-gosx-bind-style={"--gsx-dock-left:" + props.LeftSignal + ":px,--gsx-dock-right:" + props.RightSignal + ":px,--gsx-dock-bottom:" + props.BottomSignal + ":px"}
	>
		{children}
	</div>
}
