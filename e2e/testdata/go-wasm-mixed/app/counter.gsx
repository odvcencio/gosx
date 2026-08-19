package app

import "m31labs.dev/gosx/signal"

// Counter is not the island the mixed-runtime browser fixture renders (that
// one is island/program.CounterProgram, a hand-built reference bytecode
// fixture, kept byte-identical to this file on purpose). It exists so
// `gosx build --prod` has a real strict island source to discover, lower,
// and bundle from this module, proving that pipeline works for a strict
// declaration exactly as it works for the legacy spelling.
//
//gosx:island
component Counter() {
	count := signal.New(0)
	decrement := func() { count.Set(count.Get() - 1) }
	increment := func() { count.Set(count.Get() + 1) }
	return <div class="counter">
		<button onClick={decrement}>-</button>
		{count.Get()}
		<button onClick={increment}>+</button>
	</div>
}
