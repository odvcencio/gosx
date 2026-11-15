package counter

type PageProps struct {
	Count    int
	PrevHref string
	NextHref string
}

component Page(props: PageProps) {
	return <main class="container">
		<p class="eyebrow">GoSX / no client JavaScript</p>
		<h1>Counter</h1>
		<p>Each link is a full page load. The count lives in the URL, and the loader reads it on every request.</p>
		<nav aria-label="Examples">
			<a href="/">Home</a>
			<a href="/counter">Counter</a>
		</nav>
		<section class="panel">
			<div class="counter">
				<a href={props.PrevHref} aria-label="Subtract one">[ - ]</a>
				<span class="count">{props.Count}</span>
				<a href={props.NextHref} aria-label="Add one">[ + ]</a>
			</div>
		</section>
	</main>
}
