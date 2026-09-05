package app

type ChromeProps struct {
	Title string
}

component Chrome(props: ChromeProps) {
	return <main class="strict-composition-shell" data-ssr-shared="chrome">
		<h1>{props.Title}</h1>
		<section class="strict-composition-grid">{children}</section>
	</main>
}
