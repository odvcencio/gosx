package main

import ui "./ui"

component Page() {
	return <ui.Card Tone="home">{<strong slot="Title">Title</strong>}<p>Body</p></ui.Card>
}

