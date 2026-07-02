package main

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
//gosx:island
func Counter() Node {
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
