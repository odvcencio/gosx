package app

import "m31labs.dev/gosx/signal"

//gosx:island
func Sections() Node {
	visible := signal.New(false)
	toggle := func() { visible.Set(!visible.Get()) }
	return <section id="sections">
		<button id="section-toggle" onClick={toggle}>Toggle middle section</button>
		<table id="split-table">
			<tr id="split-left"><td>left</td></tr>
			<If cond={visible.Get()}><tbody id="split-middle"><tr><td>middle</td></tr></tbody></If>
			<tr id="split-right"><td>right</td></tr>
		</table>
	</section>
}
