package integers

import "m31labs.dev/gosx/signal"

type ExactCounterProps struct {
	Initial int64
}

//gosx:island
component ExactCounter(props: ExactCounterProps) {
	count := signal.New(props.Initial)
	increment := func() { count.Set(count.Get() + 1) }
	decrement := func() { count.Set(count.Get() - 1) }
	reset := func() { count.Set(props.Initial) }
	return <section aria-label="Exact integer counter">
		<p id="island-integer-value" aria-live="polite">{count.Get()}</p>
		<div>
			<button type="button" onClick={decrement}>Subtract one</button>
			<button type="button" onClick={increment}>Add one</button>
			<button type="button" onClick={reset}>Reset</button>
		</div>
	</section>
}

func Page() Node {
	return <article class="prose">
		<p class="lede">Large IDs and counters should survive the trip from Go to the browser. This counter starts beyond JavaScript's safe integer range.</p>
		<section id="integer-example" class="docs-live-example" aria-label="Exact integer island example">
			<h2>Exact counter</h2>
			<ExactCounter {...data.integerProps} />
			<a
				href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/docs/islands/integers/page.gsx"
				rel="noopener"
			>View the counter source</a>
		</section>
		<p>The server supplies Initial as int64. The shared island VM preserves signed 64-bit integers through nested JSON props, literals, signals, integer arithmetic, comparisons, and text rendering. Putting a number into a float explicitly uses floating-point precision.</p>
		<p>Integer arithmetic wraps at signed 64-bit limits. Integer division truncates toward zero; the VM returns zero for division or remainder by zero. Unsigned values above the signed 64-bit range are outside this exact-integer contract.</p>
		<p>The scalar AOT path retains its checked 32-bit integer contract. This int64 example uses the shared VM. The framework preserves the original props JSON at the browser handoff when an integer exceeds JavaScript's safe range; ordinary props retain one snapshot.</p>
		<h2 id="integer-source">Source</h2>
		{CodeBlock("gosx", data.integerSample)}
		<a href="/docs/islands/collections/">Collection editor</a>
		<a href="/docs/islands/">Island reference</a>
	</article>
}
