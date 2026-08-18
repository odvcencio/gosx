package counter

func Page() Node {
	return <div class="container">
		<h1>Counter</h1>
		<nav>
			<a href="/">Home</a> | <a href="/counter">Counter</a>
		</nav>
		<hr />
		<div class="counter">
			<a href={data.prevHref}>[ - ]</a>
			<span style="margin: 0 1em; font-size: 2em">{data.count}</span>
			<a href={data.nextHref}>[ + ]</a>
		</div>
	</div>
}
