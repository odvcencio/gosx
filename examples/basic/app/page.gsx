package app

func Page() Node {
	return <div class="container">
		<h1>GoSX Example</h1>
		<p>A Go-native web application platform.</p>
		<nav>
			<a href="/">Home</a>
			|
			<a href="/counter">Counter</a>
		</nav>
		<hr />
		<p>
			Server rendered at:
			{data.serverTime}
		</p>
	</div>
}
