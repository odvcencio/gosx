package main

import "m31labs.dev/gosx/signal"

// Counter is the island you edit to see hot-swap in action.
//
// Run `gosx dev` in this directory, open the page, bump the count a few times,
// then change something below — the label text, the step size, a class — and
// save. `gosx dev` recompiles just this island, ships the fresh bytecode over
// the dev socket, and the running island swaps in place: the count you already
// clicked up stays put, and the page never reloads.
//
// Try each of these edits and watch the live island update without a refresh:
//   - change "count is" to "clicks:" (static text swap)
//   - change `count.Get() + 1` to `+ 5` and `- 1` to `- 5` (handler swap)
//   - add a class to the <div> (attribute swap)
//
// Counter is declared with the strict `component` syntax rather than the
// legacy `func Name(...) Node` style. Lowering, `gosx check`, server
// rendering, and client hydration all treat a strict island exactly like a
// legacy one — this file is proof of that, not a special case: the compiled
// island program main.go loads through compileIsland is byte-identical in
// shape to what the legacy spelling produced, and the hot-swap flow above
// still works unchanged.
//
//gosx:island
component Counter() {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	decrement := func() { count.Set(count.Get() - 1) }
	return <div class="counter">
		<button class="counter-btn" onClick={decrement}>-</button>
		<span class="counter-label">
			count is
			{count.Get()}
		</span>
		<button class="counter-btn" onClick={increment}>+</button>
	</div>
}
