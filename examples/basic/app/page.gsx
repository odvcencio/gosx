package app

type PageProps struct {
	ServerTime string
}

component Page(props: PageProps) {
	return <main class="container">
		<p class="eyebrow">GoSX / file routes</p>
		<h1>GoSX Example</h1>
		<p>A Go-native web application platform. This page is a server-rendered .gsx file, and a Go file beside it loads its data.</p>
		<nav aria-label="Examples">
			<a href="/">Home</a>
			<a href="/counter">Counter</a>
		</nav>
		<section class="panel">
			<p>
				Server rendered at
				<span class="stamp">{props.ServerTime}</span>
			</p>
			<p><a class="docs-link" href="https://gosx.m31labs.dev/docs/routing">How file routing works</a></p>
		</section>
	</main>
}
