package counter

//gosx:island
func Counter(props CounterProps) Node {
	return <div class="counter">
		<button onClick={decrement}>-</button>
		<span class="count">{count}</span>
		<button onClick={increment}>+</button>
	</div>
}
