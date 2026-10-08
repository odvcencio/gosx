package app

import "m31labs.dev/gosx/signal"

type LinkProps struct { Start string }

//gosx:island
component RouteLink(props: LinkProps) {
	target := signal.New(props.Start)
	update := func() { target.Set("/done") }
	return <div id="url-island">
		<a id="reactive-link" href={target.Get()}>Visit</a>
		<button id="update-link" onClick={update}>Update</button>
	</div>
}

component Page() {
	return <main>
		<RouteLink start="/news" />
		<a id="model-link" href="/models/city.gltf">Model</a>
		<div id="region" data-gosx-region data-gosx-region-url="/fragment" data-gosx-region-interval="1s">waiting</div>
	</main>
}
