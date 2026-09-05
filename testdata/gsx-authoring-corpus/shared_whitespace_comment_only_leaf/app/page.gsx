package main

import ui "./ui"

component Page() {
	return <ui.Leaf>
		// comment-only child should not become caller content
	</ui.Leaf>
}

