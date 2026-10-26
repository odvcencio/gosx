package app

import "m31labs.dev/gosx/signal"

// Tally lives in a directory the page imports by relative path.
//
//gosx:island
func Tally() Node {
	count := signal.New(10)
	decrement := func() { count.Set(count.Get() - 1) }
	return <div class="tally" id="shared-tally">
		<span id="shared-count">{count.Get()}</span>
		<button id="shared-dec" onClick={decrement}>-</button>
	</div>
}
