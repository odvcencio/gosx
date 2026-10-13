package docs

func Page() Node {
	return <section
		class="bgb"
		aria-label="Blackglass Beach"
		role="region"
		data-view={data.view}
		data-period={data.period}
		data-coast-pickup={data.pickupCommands}
		data-coast-chart-places={data.chartPlaces}
		data-coast-pool-sites={data.poolRippleSites}
		data-gosx-scene3d-control-scope
		data-gosx-scene3d-status-scope
	>
		<div class="bgb__canvas gosx-scene3d-poster-stage" data-gosx-scene3d-poster-stage>
			<img
				class="gosx-scene3d-poster"
				src={data.posterSrc}
				alt=""
				width="1000"
				height="625"
				decoding="async"
				fetchpriority="high"
			 />
			<Scene3D {...data.scene} stats={false} />
		</div>
		<p class="bgb__preview-state" role="status">Still preview · interactive 3D needs JavaScript and graphics support.</p>
		<a class="bgb__close" href="/demos" aria-label="Back to the demos">×</a>
		<div class="bgb__tools">
			<details class="bgb__chart">
				<summary>Coastal chart</summary>
				<div class="bgb__chart-body">
					<p class="bgb__chart-heading">BLACKGLASS COAST <span>Survey &amp; sailing</span></p>
					<svg
						class="bgb__map"
						viewBox="-70 -80 160 120"
						role="img"
						aria-label="Coastal survey chart with live ship position"
					>
						<path class="bgb__map-coast" d={data.chartShore} />
						<g data-coast-map-places></g>
						<g data-coast-map-ship transform="translate(21 -42)"><path d="M0 -4L1.7 2L0 1L-1.7 2Z" /></g>
					</svg>
					<p class="bgb__map-legend">N ↑ · chart follows your ship · select a landmark to visit</p>
					<dl class="bgb__instruments">
						<div><dt>Heading</dt><dd data-coast-bearing>—</dd></div>
						<div><dt>Speed</dt><dd data-coast-speed>—</dd></div>
						<div><dt>Wind from</dt><dd data-coast-wind>—</dd></div>
						<div><dt>Voyage</dt><dd data-coast-distance>—</dd></div>
					</dl>
					<label class="bgb__course-label" for="coast-course">Course to hold</label>
					<div class="bgb__course-row"><input
							id="coast-course"
							data-coast-course
							type="number"
							min="0"
							max="360"
							step="1"
							value="60"
							inputmode="numeric"
						 /><span>degrees</span><button type="button" data-coast-hold disabled>Hold course</button></div>
					<div class="bgb__chart-actions"><button type="button" data-coast-chart-helm disabled>Pilot ship</button><button type="button" data-coast-chart-sail disabled>Full sail</button></div>
					<p class="bgb__chart-status" data-coast-chart-status role="status">Instruments connect when the scene is ready.</p>
					<p class="bgb__chart-hint">Manual steering takes over. Hold a course out at sea and play the Game Boy while the ship sails.</p>
					<nav class="bgb__chart-places" data-coast-chart-links aria-label="Visit a coastal landmark"></nav>
				</div>
			</details>
			<details class="bgb__journal">
				<summary>Field journal <span data-coast-found>0 / 4</span></summary>
				<p class="bgb__journal-intro">Small worlds in the basalt. Visit the pools, then click or tap the water to take a closer look.</p>
				<a class="bgb__journal-route" href={data.poolsHref}>Visit the tide pools ↗</a>
				<details data-coast-note="tide-pool-0">
					<summary>Ochre stars</summary>
					<p>Five arms against the black rock. Look for the warm coral shapes just beneath the surface.</p>
				</details>
				<details data-coast-note="tide-pool-1">
					<summary>Anemone garden</summary>
					<p>A little crown of green tentacles in a sheltered bowl. Circle the rim to catch their silhouettes.</p>
				</details>
				<details data-coast-note="tide-pool-2">
					<summary>The limpet ledge</summary>
					<p>Pale shells gather along the inner lip of the basalt. The quiet water catches a different sky at every hour.</p>
				</details>
				<p data-coast-handheld-note hidden>A violet handheld was left on the dry rock. Your pocket has room for one more discovery.</p>
				<p class="bgb__journal-status" role="status" data-coast-status>Explore at your own pace.</p>
			</details>
		</div>
		<dialog class="bgb__handheld-dialog" aria-label="Pocket handheld">
			<button class="bgb__handheld-close" type="button" data-coast-put-away>Put away · Esc</button>
			<p class="bgb__handheld-sailing" data-coast-handheld-sailing hidden>Course held · the ship is sailing</p>
		</dialog>
		<button type="button" hidden data-gosx-scene3d-animation-toggle>Pause coast</button>
		<div class="bgb__controls">
			<p class="bgb__instructions bgb__instructions-desktop">{data.instructions}</p>
			<p class="bgb__instructions bgb__instructions-touch">{data.touchInstructions}</p>
			<p class="bgb__instructions bgb__discoveries">
				Follow footprints to the glass. Wreck and hollow to the west; tide pools and lighthouse to the east. Pilot ship to explore the sea.
			</p>
			<nav class="bgb__group" aria-label="View">
				<a class="bgb__link bgb__view-shore" href={data.shoreHref}>Shore</a>
				<a class="bgb__link bgb__view-glass" href={data.glassHref}>Glass</a>
				<a class="bgb__link bgb__view-cliff" href={data.cliffHref}>Overlook</a>
				<a class="bgb__link bgb__view-pools" href={data.poolsHref}>Tide pools</a>
				<a class="bgb__link bgb__view-ship" href={data.shipHref}>Ship view</a>
				<button class="bgb__link bgb__reset" type="button" data-gosx-scene3d-reset="">Reset view</button>
			</nav>
			<nav class="bgb__group" aria-label="Light">
				<a class="bgb__link bgb__period-golden-hour" href={data.goldenHref}>Golden hour</a>
				<a class="bgb__link bgb__period-blue-hour" href={data.blueHref}>Blue hour</a>
				<a class="bgb__link bgb__period-noon" href={data.noonHref}>Midday</a>
			</nav>
			<button
				class="bgb__link bgb__pocket"
				type="button"
				data-coast-pocket
				hidden
				aria-label="Pull out the Game Boy"
			>Game Boy</button>
		</div>
		<p class="bgb__reveal" aria-live="off">
			Rendered by GoSX: Go + WASM,
			<output data-gosx-scene3d-status="bytes">…</output>
			KB,
			<output data-gosx-scene3d-status="fps">…</output>
			fps on this device
			<a href="/docs/scene3d">How it works ↗</a>
		</p>
	</section>
}
