package counter

import "m31labs.dev/gosx/signal"

type CounterProps struct {
	Initial int
}

//gosx:island
component Counter(props: CounterProps) {
	count := signal.New(props.Initial)
	increment := func() { count.Set(count.Get() + 1) }
	return <button onClick={increment}>
		Count:
		{count.Get()}
	</button>
}

func Page() Node {
	return <main class="shell">
		<h1>Counter</h1>
		<Counter {...data.counterProps} />
	</main>
}
