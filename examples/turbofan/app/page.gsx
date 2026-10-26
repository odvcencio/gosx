package app

import (
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/signal"
)

func Page() Node {
	return <main>
		<header class="masthead">
			<a href="/" class="wordmark" aria-label="Cycle Lab home"><span class="brand-mark">◎</span> CYCLE<span class="wordmark-light">/ LAB</span></a>
			<p class="edition">OBJECT STUDIES <span>№ 001</span></p>
		</header>
		<section class="introduction" aria-labelledby="title">
			<div><p class="eyebrow"><span class="accent-dot"></span> THE PROPULSION SERIES</p><h1 id="title">Inside the turbofan<span>.</span></h1></div>
			<p class="intro-copy">A machine that turns air into momentum. Open the casing, change the cycle, and measure what comes out.</p>
		</section>
		<section
			class="viewer"
			aria-label="Interactive turbofan model"
			data-gosx-scene3d-status-scope
			data-gosx-scene3d-control-scope
		>
			<div class="viewer-heading"><div><span class="model-id">TF—01</span><span class="model-description">TWO-SPOOL / HIGH-BYPASS</span></div><span class="motion-state"><span class="status-dot"></span><span class="playing-label">SLOW MOTION</span><span class="paused-label">ROTATION PAUSED</span><span class="reduced-label">REDUCED MOTION</span></span></div>
			<div class="scene-area"><Scene3D {...data.scene} stats={false} /></div>
			<div class="viewer-controls">
				<TurbofanControls View={data.view} Period={data.period} />
				<button type="button" class="motion-control" data-gosx-scene3d-animation-toggle aria-pressed="false"><span class="playing-label">Pause rotation</span><span class="paused-label">Resume rotation</span><span class="reduced-label">Reduced motion</span><span aria-hidden="true"> ↗</span></button>
			</div>
			<div class="viewer-footnote"><span>DRAG TO ORBIT <span class="separator">/</span> SCROLL OR PINCH TO ZOOM</span><span class="renderer-label">RENDERER <output data-gosx-scene3d-status="renderer">starting…</output></span></div>
			<p class="fallback" role="status" data-gosx-scene3d-status="fallback" hidden></p>
		</section>
		<CycleWorkbench />
		<section class="cycle" aria-labelledby="cycle-title">
			<div class="section-heading"><h2 id="cycle-title">One continuous cycle.</h2><p>AIR IN <span aria-hidden="true">────────→</span> THRUST OUT</p></div>
			<div class="stage-grid">
				<article class="stage"><span class="stage-number">01 <span>↗</span></span><h3>Fan &amp; bypass</h3><p>The fan accelerates air. Much of it travels around the core through the bypass duct, contributing thrust.</p><span class="stage-detail">MOVE MORE AIR</span></article>
				<article class="stage"><span class="stage-number">02 <span>→</span></span><h3>Compression</h3><p>Successive rotor and stator rows raise core pressure. Two concentric shafts let the spools turn at different speeds.</p><span class="stage-detail">RAISE THE PRESSURE</span></article>
				<article class="stage hot-stage"><span class="stage-number">03 <span>✳</span></span><h3>Combustion</h3><p>Fuel mixes with compressed air in an annular chamber. Burning adds energy before the flow reaches the turbines.</p><span class="stage-detail">ADD HEAT</span></article>
				<article class="stage"><span class="stage-number">04 <span>↗</span></span><h3>Turbine &amp; exhaust</h3><p>The turbines drive the compressor and fan. Remaining energy accelerates the exhaust through the core nozzle.</p><span class="stage-detail">EXTRACT &amp; ACCELERATE</span></article>
			</div>
		</section>
		<footer class="colophon"><p>ANATOMY, WITH NUMBERS.</p><p>Steady cycle model · Uncalibrated engine · Geometry &amp; rotor speeds illustrative</p><a href="https://github.com/odvcencio/gosx/tree/main/examples/turbofan">Built with GoSX <span aria-hidden="true">↗</span></a></footer>
	</main>
}

func CycleWorkbench() Node {
	return <section class="cycle-lab" aria-labelledby="workbench-title">
		<div class="section-heading"><h2 id="workbench-title">Put the cycle to work.</h2><p>STEADY STATE <span aria-hidden="true">/</span> DESIGN POINT</p></div>
		<p class="workbench-intro">Change flight conditions and component design. Follow the temperature rise, the shaft work, and the thrust from each stream.</p>
		<Surface
			name="TurbofanCycle"
			id="cycle-workbench"
			class="workbench"
			runtime="go-wasm"
			wasmPath={data.cycleWASMPath}
			props={data.inputs}
		>
			<p class="cycle-loading">Loading interactive cycle controls. The initial calculated point is shown below.</p>
			<div class="workbench-grid">
				<fieldset class="cycle-inputs" disabled>
					<legend>Set a design point</legend>
					<div class="cycle-presets" aria-label="Flight condition presets"><button type="button" data-cycle-preset="static">Sea level</button><button type="button" data-cycle-preset="cruise">Cruise</button><button type="button" data-cycle-preset="reset">Reset all</button></div>
					<label for="cycle-altitude"><span>Altitude</span><output for="cycle-altitude" data-cycle-input-label="altitudeM">0.0 km</output></label>
					<input
						id="cycle-altitude"
						name="altitudeM"
						type="range"
						min="0"
						max="11000"
						step="250"
						value={data.inputs.AltitudeM}
					 />
					<label for="cycle-mach"><span>Flight speed</span><output for="cycle-mach" data-cycle-input-label="mach">M 0.00</output></label>
					<input
						id="cycle-mach"
						name="mach"
						type="range"
						min="0"
						max="0.85"
						step="0.01"
						value={data.inputs.Mach}
					 />
					<label for="cycle-t4"><span>Burner exit temperature</span><output for="cycle-t4" data-cycle-input-label="turbineInletK">1600 K</output></label>
					<input
						id="cycle-t4"
						name="turbineInletK"
						type="range"
						min="1000"
						max="1800"
						step="10"
						value={data.inputs.TurbineInletK}
					 />
					<label for="cycle-opr"><span>Overall pressure ratio</span><output for="cycle-opr" data-cycle-input-label="overallPressureRatio">30 : 1</output></label>
					<input
						id="cycle-opr"
						name="overallPressureRatio"
						type="range"
						min="5"
						max="40"
						step="1"
						value={data.inputs.OverallPressureRatio}
					 />
					<label for="cycle-fpr"><span>Fan pressure ratio</span><output for="cycle-fpr" data-cycle-input-label="fanPressureRatio">1.50 : 1</output></label>
					<input
						id="cycle-fpr"
						name="fanPressureRatio"
						type="range"
						min="1.1"
						max="1.8"
						step="0.01"
						value={data.inputs.FanPressureRatio}
					 />
					<label for="cycle-bypass"><span>Bypass ratio</span><output for="cycle-bypass" data-cycle-input-label="bypassRatio">8.0 : 1</output></label>
					<input
						id="cycle-bypass"
						name="bypassRatio"
						type="range"
						min="1"
						max="12"
						step="0.5"
						value={data.inputs.BypassRatio}
					 />
					<p class="input-note">Each change solves a new design point. It does not simulate throttling a fixed engine.</p>
				</fieldset>
				<div class="cycle-results">
					<dl class="cycle-metrics">
						<div><dt>Net thrust</dt><dd><output data-cycle-output="net-thrust">{data.cycle.Values["net-thrust"]}</output><span>kN</span></dd></div>
						<div><dt>Fuel flow</dt><dd><output data-cycle-output="fuel-flow">{data.cycle.Values["fuel-flow"]}</output><span>kg/s</span></dd></div>
						<div><dt>Specific fuel consumption</dt><dd><output data-cycle-output="tsfc">{data.cycle.Values["tsfc"]}</output><span>mg/(N·s)</span></dd></div>
						<div><dt>Thrust from bypass</dt><dd><output data-cycle-output="bypass-share">{data.cycle.Values["bypass-share"]}</output><span>%</span></dd></div>
					</dl>
					<figure class="cycle-plot">
						<figcaption>Core total temperature <span>KELVIN</span></figcaption>
						<svg
							viewBox="0 0 720 230"
							role="img"
							aria-label="Core total temperature through the inlet, fan, compressor, burner, high pressure turbine, and low pressure turbine"
						>
							<g class="plot-grid"><path d="M40 28H680 M40 114H680 M40 200H680" /></g>
							<g class="plot-labels"><text x="0" y="32">1800</text><text x="7" y="118">900</text><text x="22" y="204">0</text><text x="40" y="225">2</text><text x="168" y="225">13</text><text x="296" y="225">3</text><text x="424" y="225">4</text><text x="552" y="225">45</text><text x="680" y="225">5</text></g>
							<path class="temperature-line" data-cycle-plot d={data.cycle.TemperaturePath} />
						</svg>
					</figure>
					<div class="shaft-powers"><p><span>Fan / LP shaft</span><strong data-cycle-output="fan-power">{data.cycle.Values["fan-power"]}</strong> MW</p><p><span>Compressor / HP shaft</span><strong data-cycle-output="compressor-power">{data.cycle.Values["compressor-power"]}</strong> MW</p></div>
				</div>
			</div>
			<div class="cycle-status-row"><p class="balance-status" data-cycle-output="status" role="status">{data.cycle.Values["status"]}</p><button type="button" class="cycle-export" data-cycle-export disabled>Export point · CSV <span aria-hidden="true">↗</span></button></div>
			<details class="cycle-details" open>
				<summary>Station conditions &amp; nozzle sizing</summary>
				<div class="station-scroll" tabindex="0" role="region" aria-label="Scrollable station conditions"><table class="station-table"><caption>Total temperature and pressure, except nozzle exit rows which show static conditions.</caption><thead><tr><th scope="col">Station</th><th scope="col">T · K</th><th scope="col">P · kPa</th><th scope="col">Flow · kg/s</th></tr></thead><tbody>
							<Each of={data.cycle.Stations} as="station"><tr data-cycle-station={station.ID}><th scope="row">{station.Name}</th><td data-quantity="temperature">{station.T}</td><td data-quantity="pressure">{station.P}</td><td data-quantity="flow">{station.Flow}</td></tr></Each>
						</tbody></table></div>
				<div class="nozzle-grid"><p>Core nozzle <strong data-cycle-output="core-nozzle">{data.cycle.Values["core-nozzle"]}</strong><span>Exit area <output data-cycle-output="core-area">{data.cycle.Values["core-area"]}</output> m²</span></p><p>Bypass nozzle <strong data-cycle-output="bypass-nozzle">{data.cycle.Values["bypass-nozzle"]}</strong><span>Exit area <output data-cycle-output="bypass-area">{data.cycle.Values["bypass-area"]}</output> m²</span></p></div>
			</details>
			<details class="cycle-details assumptions">
				<summary>Model assumptions &amp; what these numbers mean</summary>
				<p>A one-dimensional, steady design-point Brayton cycle with separate core and bypass streams. Both shafts balance turbine work against their loads. Ideal convergent nozzles include choking and pressure thrust; their areas resize to pass the prescribed flow at every point.</p>
				<p>Constant γ = 1.4, R = 287.05 J/(kg·K), cp = 1004.675 J/(kg·K). ISA troposphere. Corrected core airflow = 40 kg/s at 288.15 K and 101.325 kPa, referenced to inlet total conditions. Fan/compressor/turbine efficiencies = 0.90/0.88/0.91; shaft and combustion efficiencies = 0.99. Inlet/burner pressure recovery = 0.99/0.95. Fuel LHV = 43 MJ/kg.</p>
				<p>Useful for cycle trends and energy accounting. This generic model has no engine calibration, component maps, surge margin, cooling, variable gas properties, shaft inertia, or fixed-geometry off-design matching. It does not predict a real engine’s performance or the rotor RPM shown above.</p>
				<p>Equations: <a href="https://www.grc.nasa.gov/www/k-12/airplane/compress.html">NASA compressor</a>, <a href="https://www.grc.nasa.gov/www/k-12/airplane/burnth.html">burner</a>, <a href="https://www.grc.nasa.gov/www/k-12/airplane/powtrbth.html">turbine</a>, <a href="https://www.grc.nasa.gov/www/k-12/airplane/isentrop.html">isentropic flow</a>, and <a href="https://www1.grc.nasa.gov/beginners-guide-to-aeronautics/thrust-force/">thrust</a>. Largest relative conservation residual at this point: <output data-cycle-output="residual">{data.cycle.Values["residual"]}</output>.</p>
			</details>
		</Surface>
	</section>
}

//gosx:island
func TurbofanControls(props struct{ View, Period string }) Node {
	request := signal.NewShared("turbofan.presentation", scene.PresentationRequest{})
	state := signal.NewShared("turbofan.presentationState", scene.PresentationState{View: props.View, Period: props.Period})
	selectView := func() { request.Set(scene.PresentationRequest{View: eventData, Revision: request.Get().Revision + 1}) }
	selectCasing := func() { request.Set(scene.PresentationRequest{Period: eventData, Revision: request.Get().Revision + 1}) }
	return <div class="presentation-controls">
		<nav class="segmented" aria-label="Casing view">
			<button
				type="button"
				onClick={selectCasing}
				data-gosx-event-value="cutaway"
				disabled={!state.Get().Ready}
				aria-pressed={state.Get().Period=="cutaway"}
			>Cutaway</button>
			<button
				type="button"
				onClick={selectCasing}
				data-gosx-event-value="exterior"
				disabled={!state.Get().Ready}
				aria-pressed={state.Get().Period=="exterior"}
			>Exterior</button>
		</nav>
		<nav class="camera-controls" aria-label="Camera presets">
			<button
				type="button"
				onClick={selectView}
				data-gosx-event-value="perspective"
				disabled={!state.Get().Ready}
				aria-pressed={state.Get().View=="perspective"}
			>Perspective</button>
			<button
				type="button"
				onClick={selectView}
				data-gosx-event-value="side"
				disabled={!state.Get().Ready}
				aria-pressed={state.Get().View=="side"}
			>Side</button>
			<button
				type="button"
				onClick={selectView}
				data-gosx-event-value="intake"
				disabled={!state.Get().Ready}
				aria-pressed={state.Get().View=="intake"}
			>Intake</button>
		</nav>
	</div>
}
