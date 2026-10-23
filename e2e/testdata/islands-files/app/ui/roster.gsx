package app

import "m31labs.dev/gosx/signal"

// Roster patches rows inside two tables: one with thead and tfoot beside the
// rows, one made only of keyed rows.
//
//gosx:island
func Roster() Node {
	names := signal.New([]string{"ada", "bo", "cy"})
	add := func() { names.Set(append(names.Get(), "di")) }
	remove := func() { names.Set([]string{"ada", "cy"}) }
	reorder := func() { names.Set([]string{"cy", "ada", "bo"}) }
	return <section id="roster">
		<button id="roster-add" onClick={add}>Add</button>
		<button id="roster-remove" onClick={remove}>Remove</button>
		<button id="roster-reorder" onClick={reorder}>Reorder</button>
		<table id="full">
			<thead><tr><th>name</th></tr></thead>
			<Each of={names.Get()} as="name" key={name}><tr class="row"><td>{name}</td></tr></Each>
			<tfoot><tr><td>{len(names.Get())} names</td></tr></tfoot>
		</table>
		<table id="bare">
			<Each of={names.Get()} as="name" key={name}><tr class="row"><td>{name}</td></tr></Each>
		</table>
	</section>
}
