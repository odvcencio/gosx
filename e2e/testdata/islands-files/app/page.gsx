package app

import ui "./ui"

func Page() Node {
	return <main class="shell">
		<h1>Islands in other files</h1>
		<Counter />
		<ui.Tally />
		<Chart />
		<ForeignContent />
		<ui.Roster />
		<ui.Sections />
		<ImportedMap />
	</main>
}
