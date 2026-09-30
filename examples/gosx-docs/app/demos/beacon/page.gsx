package docs

func Page() Node {
	return <section
		class="bgb"
		aria-label="Blackglass Beach"
		role="region"
		data-view={data.view}
		data-period={data.period}
		data-gosx-scene3d-control-scope
		data-gosx-scene3d-status-scope
	>
		<div class="bgb__canvas">
			<Scene3D {...data.scene} />
		</div>
		<a class="bgb__close" href="/demos" data-gosx-link="true" aria-label="Back to the demos">×</a>
		<div class="bgb__controls">
			<nav class="bgb__group" aria-label="View">
				<a class="bgb__link bgb__view-shore" href={data.shoreHref} data-gosx-link="true">Shore</a>
				<a class="bgb__link bgb__view-glass" href={data.glassHref} data-gosx-link="true">Glass</a>
				<a class="bgb__link bgb__view-cliff" href={data.cliffHref} data-gosx-link="true">Cliff</a>
				<button class="bgb__link bgb__reset" type="button" data-gosx-scene3d-reset="">Reset view</button>
			</nav>
			<nav class="bgb__group" aria-label="Light">
				<a class="bgb__link bgb__period-golden-hour" href={data.goldenHref} data-gosx-link="true">Golden hour</a>
				<a class="bgb__link bgb__period-blue-hour" href={data.blueHref} data-gosx-link="true">Blue hour</a>
				<a class="bgb__link bgb__period-noon" href={data.noonHref} data-gosx-link="true">Noon</a>
			</nav>
		</div>
	</section>
}
