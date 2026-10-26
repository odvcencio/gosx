package app

import "m31labs.dev/gosx/signal"

// Counter lives beside page.gsx and is used as <Counter />.
//
//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	return <div class="counter" id="sibling-counter">
		<span id="sibling-count">{count.Get()}</span>
		<button id="sibling-inc" onClick={increment}>+</button>
	</div>
}
