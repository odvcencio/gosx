package ui

type TabsProps struct {
	ID     string
	Signal string
	Label  string
}

// Place TabPanel children in slot="Panels"; links use the default children slot.
component Tabs(props: TabsProps) {
	return <div id={props.ID} data-gosx-tabs data-gosx-tabs-signal={props.Signal}>
		<nav class="gsx-tabs__list" aria-label={props.Label}>{children}</nav>
		{slotPanels}
	</div>
}

type TabProps struct {
	ID       string
	Href     string
	Panel    string
	Selected bool
}

component Tab(props: TabProps) {
	return <>
		<If cond={props.Selected}>
			<a
				id={props.ID}
				class="gsx-tabs__tab"
				href={props.Href}
				data-gosx-tab={props.ID}
				data-gosx-tab-panel={props.Panel}
				aria-current="page"
			>{children}</a>
		</If>
		<If cond={props.Selected == false}>
			<a
				id={props.ID}
				class="gsx-tabs__tab"
				href={props.Href}
				data-gosx-tab={props.ID}
				data-gosx-tab-panel={props.Panel}
			>{children}</a>
		</If>
	</>
}

type TabPanelProps struct {
	ID     string
	Hidden bool
}

component TabPanel(props: TabPanelProps) {
	return <section id={props.ID} class="gsx-tabs__panel" hidden={props.Hidden}>{children}</section>
}
