package docs

func Page() Node {
	return <section
		class="beacon"
		aria-label="Blackglass Coast"
		role="region"
		data-view={data.view}
		data-period={data.period}
		data-gosx-scene3d-status-scope
		data-gosx-scene3d-control-scope
	>
		<div class="beacon__canvas">
			<Scene3D {...data.scene} />
		</div>
		<form
			class="beacon__ripple-control"
			aria-hidden="true"
			data-gosx-scene3d-control-form="water-tap"
			data-gosx-scene3d-control-subject="blackglass-cove"
		></form>
		<header class="beacon__intro">
			<p class="beacon__eyebrow">
				Studio world ·
				{data.viewName}
				·
				{data.periodName}
			</p>
			<h1>Blackglass Coast</h1>
			<p class="beacon__copy">
				Explore a volcanic cove. A live tide flows between basalt shelves. A ruined arch marks the shore. The beacon burns above the far terrace.
			</p>
			<div class="beacon__telemetry" aria-live="polite">
				<p>
					<span>Renderer</span>
					<output data-gosx-scene3d-status="renderer">starting…</output>
					<output data-gosx-scene3d-status="fallback" hidden></output>
				</p>
				<p>
					<span>Quality</span>
					<output data-gosx-scene3d-status="quality">measuring…</output>
				</p>
			</div>
		</header>
		<aside class="beacon__dock" aria-label="Coast controls">
			<div class="beacon__dock-row">
				<div class="beacon__dock-group">
					<p>View</p>
					<nav aria-label="Camera view">
						<a class="beacon__view-overlook" href={data.overlookHref}>Overlook</a>
						<a class="beacon__view-arrival" href={data.arrivalHref}>Arrival beach</a>
						<a class="beacon__view-beacon" href={data.beaconHref}>Beacon terrace</a>
					</nav>
				</div>
				<div class="beacon__dock-group">
					<p>Light</p>
					<nav aria-label="Light period">
						<a class="beacon__period-daybreak" href={data.daybreakHref}>Daybreak</a>
						<a class="beacon__period-high-sun" href={data.highSunHref}>High sun</a>
						<a class="beacon__period-ember-hour" href={data.emberHref}>Ember hour</a>
					</nav>
				</div>
			</div>
			<p class="beacon__controls">
				Drag or swipe to orbit · scroll or pinch to zoom · tap the water for ripples
			</p>
			<details class="beacon__facts">
				<summary>Render limits and keyboard controls</summary>
				<p>
					60 FPS cap · DPR ≤ 1.5 · 720p scene · 540p effects · 512px shadow · 128² tide grid · ≤320 embers
				</p>
				<p>
					Use the arrow keys to explore. Use + or − to zoom. Press Home to restore this view.
				</p>
			</details>
		</aside>
	</section>
}
