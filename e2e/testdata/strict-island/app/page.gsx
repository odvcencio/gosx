package app

import "m31labs.dev/gosx/signal"

type CounterProps struct {
	Label string
	Start int
}

// Counter is a strict island: its props cross the server/client boundary
// through the same proof route/fileprogram.go's localComponentProps runs
// for every strict component, then travel to the browser as the flat JSON
// map every island already ships. Page calls it with named attributes,
// exercising that boundary the same way a production caller would.
//
//gosx:island
component Counter(props: CounterProps) {
	count := signal.New(props.Start)
	increment := func() { count.Set(count.Get() + 1) }
	return <div class="counter" id="strict-counter">
		<span class="counter-label">{props.Label}</span>
		<button class="counter-btn" onClick={increment} id="strict-counter-button">{count.Get()}</button>
	</div>
}

component Page() {
	return <main class="shell">
		<h1>Strict island e2e fixture</h1>
		<Counter label="Draft Pick" start={7} />
	</main>
}
