package main

import ui "./ui"

component Page() {
	return <ui.Frame><p>Body</p><ui.Badge slot="Title" /></ui.Frame>
}

