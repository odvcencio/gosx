package main

import ui "./ui"

component Page() {
	return <ui.Frame>{<strong slot="Aside">Aside</strong>}<p>Body</p></ui.Frame>
}

