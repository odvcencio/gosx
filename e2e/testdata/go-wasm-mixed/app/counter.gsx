package app

//gosx:island
func Counter() Node {
	count := signal.New(0)
	decrement := func() { count.Set(count.Get() - 1) }
	increment := func() { count.Set(count.Get() + 1) }
	return <div class="counter">
		<button onClick={decrement}>-</button>
		{count.Get()}
		<button onClick={increment}>+</button>
	</div>
}
