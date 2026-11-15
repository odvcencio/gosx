package main

import "m31labs.dev/gosx/signal"

// CounterProps is the island's typed props. The server passes the starting
// count, so a reload with ?count=5 renders 5 before the browser takes over.
type CounterProps struct {
	Initial int
}

//gosx:island
component Counter(props: CounterProps) {
	count := signal.New(props.Initial)
	increment := func() { count.Set(count.Get() + 1) }
	decrement := func() { count.Set(count.Get() - 1) }
	return <div class="counter">
		<button onClick={decrement}>-</button>
		<span class="count">{count.Get()}</span>
		<button onClick={increment}>+</button>
	</div>
}
