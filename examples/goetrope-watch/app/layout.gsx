package watch

func Layout() Node {
	return <div class="watch-app">
		<div class="watch-app__halo"></div>
		<header class="watch-app__bar">
			<div class="watch-app__brand">
				<span class="eyebrow">Goetrope x GoSX</span>
				<a href="/" data-gosx-link class="brand">Server-driven watch surface</a>
			</div>
			<nav class="watch-app__nav" aria-label="Prototype navigation">
				<a href="/" data-gosx-link class="nav-link">Overview</a>
				<a href="/watch/alpha" data-gosx-link class="nav-link">Open room alpha</a>
			</nav>
		</header>
		<main class="watch-app__main">
			<Slot />
		</main>
		<footer class="watch-app__foot">
			The page shell, queue summary, and subtitle state are rendered on the server. Only the future transport islands should stay client-side.
		</footer>
	</div>
}
