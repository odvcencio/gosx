package docs

import (
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/signal"
)

func Page() Node {
	return <section
		class="bgb"
		aria-label="Blackglass Beach"
		role="region"
		data-view={data.view}
		data-period={data.period}
		data-revealed="true"
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
			<CoastChart
				DriftX={data.chartIslands.driftX}
				DriftZ={data.chartIslands.driftZ}
				BellX={data.chartIslands.bellX}
				BellZ={data.chartIslands.bellZ}
				StarX={data.chartIslands.starglassX}
				StarZ={data.chartIslands.starglassZ}
			 />
			<CoastJournal />
			<CoastSpyglass />
		</div>
		<button type="button" hidden data-gosx-scene3d-animation-toggle>Pause coast</button>
		<div id="coast-controls" class="bgb__controls">
			<p class="bgb__instructions bgb__instructions-desktop">{data.instructions}</p>
			<p class="bgb__instructions bgb__instructions-touch">{data.touchInstructions}</p>
			<p class="bgb__instructions bgb__discoveries">
				Tide pools ashore. Three islands offshore. Plot a course, step ashore, and bring discoveries back in your journal.
			</p>
			<CoastViews view={data.view} period={data.period} />
			<CoastPocket />
			<CoastDay />
		</div>
		<CoastMetrics />
	</section>
}

//gosx:island
func CoastSpyglass() Node {
	enabled := signal.NewShared("blackglass.spyglass", false)
	sight := signal.NewShared("blackglass.sight", scene.OpticsState{BearingLabel: "000° N", LandmarkName: "Open sea", RangeLabel: "Drag to look · bearings follow your view"})
	destination := signal.NewShared("blackglass.destination", "")
	chartOpen := signal.NewShared("blackglass.chartOpen", false)
	journalOpen := signal.NewShared("blackglass.journalOpen", false)
	raise := func() { chartOpen.Set(false); journalOpen.Set(false); enabled.Set(!enabled.Get()) }
	putAway := func() { enabled.Set(false) }
	plot := func() {
		destination.Set(sight.Get().LandmarkID)
		chartOpen.Set(true)
		journalOpen.Set(false)
		enabled.Set(false)
	}

	return <div class="bgb__optics">
		<button
			class="bgb__spyglass-toggle"
			type="button"
			aria-pressed={enabled.Get()}
			aria-controls="coast-spyglass"
			disabled={!sight.Get().Ready}
			onClick={raise}
		>Spyglass</button>
		<section
			class="bgb__spyglass"
			id="coast-spyglass"
			aria-label="Spyglass instruments"
			hidden={!enabled.Get()}
		>
			<div class="bgb__spyglass-lens" aria-hidden="true"><span></span></div>
			<div class="bgb__spyglass-instruments">
				<p class="bgb__spyglass-label">COASTAL SPYGLASS <span>3×</span></p>
				<output class="bgb__spyglass-bearing" aria-label="Looking bearing">{sight.Get().BearingLabel}</output>
				<p>{sight.Get().LandmarkName}</p>
				<p class="bgb__spyglass-range">{sight.Get().RangeLabel}</p>
				<button type="button" disabled={!sight.Get().CanNavigate} onClick={plot}>Plot island landing</button>
				<button type="button" onClick={putAway}>Put away · Esc</button>
			</div>
		</section>
	</div>
}

//gosx:island
func CoastViews(props struct{ View, Period string }) Node {
	request := signal.NewShared("blackglass.presentation", scene.PresentationRequest{})
	state := signal.NewShared("blackglass.presentationState", scene.PresentationState{View: props.View, Period: props.Period})
	selectView := func() { request.Set(scene.PresentationRequest{View: eventData, Revision: request.Get().Revision + 1}) }
	selectPeriod := func() {
		request.Set(scene.PresentationRequest{Period: eventData, Revision: request.Get().Revision + 1})
	}
	reset := func() {
		request.Set(scene.PresentationRequest{View: state.Get().View, Reset: true, Revision: request.Get().Revision + 1})
	}
	return <div class="bgb__view-controls">
		<nav class="bgb__group" aria-label="View">
			<button
				type="button"
				class="bgb__link"
				onClick={selectView}
				data-gosx-event-value="shore"
				aria-pressed={state.Get().View=="shore"}
			>Shore</button>
			<button
				type="button"
				class="bgb__link"
				onClick={selectView}
				data-gosx-event-value="glass"
				aria-pressed={state.Get().View=="glass"}
			>Glass</button>
			<button
				type="button"
				class="bgb__link"
				onClick={selectView}
				data-gosx-event-value="cliff"
				aria-pressed={state.Get().View=="cliff"}
			>Overlook</button>
			<button
				type="button"
				class="bgb__link"
				onClick={selectView}
				data-gosx-event-value="pools"
				aria-pressed={state.Get().View=="pools"}
			>Tide pools</button>
			<button
				type="button"
				class="bgb__link"
				onClick={selectView}
				data-gosx-event-value="ship"
				aria-pressed={state.Get().View=="ship"}
			>Ship view</button>
			<button type="button" class="bgb__link bgb__reset" onClick={reset}>Reset view</button>
		</nav>
		<nav class="bgb__group" aria-label="Light">
			<button
				type="button"
				class="bgb__link"
				onClick={selectPeriod}
				data-gosx-event-value="golden-hour"
				title="Jump to late afternoon"
			>Golden hour</button>
			<button
				type="button"
				class="bgb__link"
				onClick={selectPeriod}
				data-gosx-event-value="blue-hour"
				title="Jump to dusk"
			>Blue hour</button>
			<button
				type="button"
				class="bgb__link"
				onClick={selectPeriod}
				data-gosx-event-value="noon"
				title="Jump to midday"
			>Midday</button>
		</nav>
	</div>
}

//gosx:island
func CoastChart(props struct{ DriftX, DriftZ, BellX, BellZ, StarX, StarZ float64 }) Node {
	nav := signal.NewShared("blackglass.navigation", scene.NavigationState{BearingLabel: "—", SpeedLabel: "—", WindLabel: "—", DistanceLabel: "—", HelmLabel: "Pilot ship", SailLabel: "Full sail", HoldLabel: "Hold course", LandingLabel: "Step ashore", Status: "Instruments connect when the scene is ready.", Destination: "Three islands wait offshore.", MapViewBox: "-70 -80 160 120", MapTransform: "translate(21 -42)"})
	command := signal.NewShared("blackglass.vesselCommand", scene.VesselCommand{})
	destination := signal.NewShared("blackglass.destination", "")
	course := signal.New("60")
	overview := signal.New(false)
	chartOpen := signal.NewShared("blackglass.chartOpen", false)
	journalOpen := signal.NewShared("blackglass.journalOpen", false)
	toggleChart := func() { browser.PreventDefault(); chartOpen.Set(!chartOpen.Get()); journalOpen.Set(false) }
	plot := func() { browser.PreventDefault(); destination.Set(eventData) }
	edit := func() { course.Set(value); destination.Set("") }
	helm := func() { command.Set(scene.VesselCommand{Action: "helm", Revision: command.Get().Revision + 1}) }
	sailUp := func() {
		command.Set(scene.VesselCommand{Action: "trim", Trim: 1, Revision: command.Get().Revision + 1})
	}
	sailDown := func() {
		command.Set(scene.VesselCommand{Action: "trim", Trim: 0, Revision: command.Get().Revision + 1})
	}
	land := func() {
		command.Set(scene.VesselCommand{Action: "land", Revision: command.Get().Revision + 1})
		chartOpen.Set(false)
	}
	survey := func() { overview.Set(!overview.Get()) }
	hold := func() {
		command.Set(scene.VesselCommand{Action: "course", BearingInput: course.Get(), Revision: command.Get().Revision + 1})
	}
	release := func() {
		command.Set(scene.VesselCommand{Action: "release-course", Revision: command.Get().Revision + 1})
	}
	return <details class="bgb__chart" open={chartOpen.Get()}>
		<summary onClick={toggleChart}>Coastal chart</summary>
		<div class="bgb__chart-body">
			<p class="bgb__chart-heading">BLACKGLASS COAST <span>Survey &amp; sailing</span></p>
			<svg
				class="bgb__map"
				viewBox={overview.Get()?"-245 -430 600 480":nav.Get().MapViewBox}
				role="img"
				aria-label="Coastal survey chart with live ship position"
			>
				<image href="/api/blackglass-chart.svg" x="-245" y="-430" width="600" height="480" />
				<a
					href="#coast-course"
					onClick={plot}
					data-gosx-event-value="drift-isle"
					aria-label="Plot Drift Isle"
				><circle cx={props.DriftX} cy={props.DriftZ} r="1.2" /><text x={props.DriftX+2} y={props.DriftZ-2}>Drift Isle</text></a>
				<a
					href="#coast-course"
					onClick={plot}
					data-gosx-event-value="bell-rock"
					aria-label="Plot Bell Rock"
				><circle cx={props.BellX} cy={props.BellZ} r="1.2" /><text x={props.BellX+2} y={props.BellZ-2}>Bell Rock</text></a>
				<a
					href="#coast-course"
					onClick={plot}
					data-gosx-event-value="starglass"
					aria-label="Plot Starglass Isle"
				><circle cx={props.StarX} cy={props.StarZ} r="1.2" /><text x={props.StarX+2} y={props.StarZ-2}>Starglass Isle</text></a>
				<g data-coast-map-ship transform={nav.Get().MapTransform+(overview.Get()?" scale(3)":"")}><path d="M0 -4L1.7 2L0 1L-1.7 2Z" /></g>
			</svg>
			<p class="bgb__map-legend">N ↑ · select an island to plot its landing</p>
			<button type="button" onClick={survey} aria-pressed={overview.Get()}>Show archipelago</button>
			<dl class="bgb__instruments">
				<div><dt>Heading</dt><dd>{nav.Get().BearingLabel}</dd></div>
				<div><dt>Speed</dt><dd>{nav.Get().SpeedLabel}</dd></div>
				<div><dt>Wind from</dt><dd>{nav.Get().WindLabel}</dd></div>
				<div><dt>Voyage</dt><dd>{nav.Get().DistanceLabel}</dd></div>
			</dl>
			<label class="bgb__course-label" for="coast-course">Course to hold</label>
			<div class="bgb__course-row"><input
					id="coast-course"
					type="number"
					min="0"
					max="360"
					step="1"
					inputmode="numeric"
					value={course.Get()}
					onInput={edit}
				 /><span>degrees</span><button type="button" onClick={hold} disabled={!nav.Get().Sailing} hidden={nav.Get().Held}>Hold course</button><button type="button" onClick={release} hidden={!nav.Get().Held}>Release course</button></div>
			<div class="bgb__chart-actions"><button type="button" onClick={helm} disabled={!nav.Get().Ready}>{nav.Get().HelmLabel}</button><button type="button" onClick={sailUp} disabled={!nav.Get().Sailing} hidden={nav.Get().Trim>0.95}>Full sail</button><button type="button" onClick={sailDown} hidden={nav.Get().Trim<=0.95}>Lower sail</button></div>
			<button type="button" onClick={land} disabled={!nav.Get().CanLand}>{nav.Get().LandingLabel}</button>
			<p class="bgb__chart-destination" role="status">{nav.Get().Destination}</p>
			<p class="bgb__chart-status" role="status">{nav.Get().Status}</p>
			<p class="bgb__chart-hint">Hold the plotted course to sail to an island. Sails lower near its pier; slow down, step ashore, then walk to the find. Manual steering takes over at any time.</p>
			<nav class="bgb__chart-places" aria-label="Plot an island landing">
				<button type="button" onClick={plot} data-gosx-event-value="drift-isle">Drift Isle</button>
				<button type="button" onClick={plot} data-gosx-event-value="bell-rock">Bell Rock</button>
				<button type="button" onClick={plot} data-gosx-event-value="starglass">Starglass Isle</button>
			</nav>
		</div>
	</details>
}

//gosx:island
func CoastJournal() Node {
	journalOpen := signal.NewShared("blackglass.journalOpen", false)
	chartOpen := signal.NewShared("blackglass.chartOpen", false)
	toggleJournal := func() { browser.PreventDefault(); journalOpen.Set(!journalOpen.Get()); chartOpen.Set(false) }
	journal := signal.NewShared("blackglass.journal", scene.JournalState{CountLabel: "0 / 7", Notice: "Explore at your own pace."})
	request := signal.NewShared("blackglass.presentation", scene.PresentationRequest{})
	pools := func() { request.Set(scene.PresentationRequest{View: "pools", Revision: request.Get().Revision + 1}) }
	return <details class="bgb__journal" open={journalOpen.Get()}>
		<summary onClick={toggleJournal}>Field journal <span>{journal.Get().CountLabel}</span></summary>
		<p class="bgb__journal-intro">Small worlds in the basalt. Visit the pools, then click or tap the water to take a closer look.</p>
		<button type="button" class="bgb__journal-route" onClick={pools}>Visit the tide pools ↗</button>
		<details
			data-coast-note="tide-pool-0"
			data-found={journal.Get().Found["tide-pool-0"]}
			open={journal.Get().ActiveID=="tide-pool-0"}
		>
			<summary>Ochre stars</summary>
			<p>Five arms against the black rock. Look for the warm coral shapes just beneath the surface.</p>
		</details>
		<details
			data-coast-note="tide-pool-1"
			data-found={journal.Get().Found["tide-pool-1"]}
			open={journal.Get().ActiveID=="tide-pool-1"}
		>
			<summary>Anemone garden</summary>
			<p>A little crown of green tentacles in a sheltered bowl. Circle the rim to catch their silhouettes.</p>
		</details>
		<details
			data-coast-note="tide-pool-2"
			data-found={journal.Get().Found["tide-pool-2"]}
			open={journal.Get().ActiveID=="tide-pool-2"}
		>
			<summary>The limpet ledge</summary>
			<p>Pale shells gather along the inner lip of the basalt. The quiet water catches a different sky at every hour.</p>
		</details>
		<p data-coast-handheld-note hidden={!journal.Get().Pocket}>A violet handheld was left on the dry rock. Your pocket has room for one more discovery.</p>
		<details
			data-coast-note="drift-letter"
			data-found={journal.Get().Found["drift-letter"]}
			open={journal.Get().ActiveID=="drift-letter"}
		>
			<summary>The drift letter · Drift Isle</summary>
			<p>A salt-stained cache carries a sailor’s sketch: “The bell is west of the current. Beyond it, the glass remembers the stars.” Read it at the island’s crest.</p>
		</details>
		<details
			data-coast-note="tide-bell"
			data-found={journal.Get().Found["tide-bell"]}
			open={journal.Get().ActiveID=="tide-bell"}
		>
			<summary>The tide bell · Bell Rock</summary>
			<p>A bronze bell faces the empty sea. Strike it on the ridge and listen to its fading note over the surf.</p>
		</details>
		<details
			data-coast-note="star-dial"
			data-found={journal.Get().Found["star-dial"]}
			open={journal.Get().ActiveID=="star-dial"}
		>
			<summary>The star dial · Starglass Isle</summary>
			<p>Eight dark rays are set into weathered bronze. A surveyor’s compass left on a grassy crown, pointing back toward Blackglass Coast.</p>
		</details>
		<p class="bgb__journal-status" role="status" data-coast-status>{journal.Get().Notice}</p>
	</details>
}

//gosx:island
func CoastPocket() Node {
	journal := signal.NewShared("blackglass.journal", scene.JournalState{})
	// Either island can hydrate first; match CoastChart's instrument defaults.
	nav := signal.NewShared("blackglass.navigation", scene.NavigationState{BearingLabel: "—", SpeedLabel: "—", WindLabel: "—", DistanceLabel: "—", HelmLabel: "Pilot ship", SailLabel: "Full sail", HoldLabel: "Hold course", LandingLabel: "Step ashore", Status: "Instruments connect when the scene is ready.", Destination: "Three islands wait offshore.", MapViewBox: "-70 -80 160 120", MapTransform: "translate(21 -42)"})
	open := signal.NewShared("blackglass.pocket", false)
	takeOut := func() { open.Set(true) }
	putAway := func() { open.Set(false) }
	return <div class="bgb__pocket-tools">
		<button
			class="bgb__link bgb__pocket"
			type="button"
			onClick={takeOut}
			hidden={!journal.Get().Pocket}
			aria-label="Pull out the Game Boy"
		>Game Boy</button>
		<dialog id="coast-handheld" class="bgb__handheld-dialog" aria-label="Pocket handheld">
			<button class="bgb__handheld-close" type="button" onClick={putAway}>Put away · Esc</button>
			<p class="bgb__handheld-sailing" hidden={!nav.Get().Held}>Course held · the ship is sailing</p>
		</dialog>
	</div>
}

//gosx:island
func CoastDay() Node {
	automatic := signal.NewShared("blackglass.timeAutomatic", true)
	day := signal.NewShared("blackglass.day", scene.DayState{HourLabel: "17:30", WeatherLabel: "Coastal clouds", Automatic: true})
	toggle := func() { automatic.Set(!automatic.Get()) }
	return <div class="bgb__day"><span>{day.Get().HourLabel} · {day.Get().WeatherLabel}</span><button type="button" class="bgb__link" onClick={toggle} aria-pressed={automatic.Get()}>{automatic.Get()?"Hold this light":"Resume day"}</button></div>
}

//gosx:island
func CoastMetrics() Node {
	metrics := signal.NewShared("blackglass.metrics", scene.RuntimeMetrics{TransferLabel: "…", FPSLabel: "…"})
	return <p class="bgb__reveal" hidden={!metrics.Get().Ready}>Rendered by GoSX: Go + WASM, <output>{metrics.Get().TransferLabel}</output> KB transferred, <output>{metrics.Get().FPSLabel}</output> fps on this device <a href="/docs/scene3d">How it works ↗</a></p>
}
