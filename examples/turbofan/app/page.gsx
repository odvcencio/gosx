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
			data-render-model={data.mode}
		>
			<div class="viewer-heading"><div><span class="model-id">TF—01</span><span class="model-description">TWO-SPOOL / HIGH-BYPASS</span></div><span class="motion-state"><span class="status-dot"></span><span class="playing-label">SLOW MOTION</span><span class="paused-label">ROTATION PAUSED</span><span class="reduced-label">REDUCED MOTION</span></span></div>
			<div class="scene-area"><Scene3D {...data.scene} stats={false} /></div>
			<div class="render-telemetry" aria-label="Rendered operating point">
				<p><span>N1 / FAN</span><output data-render-output="n1">{data.visual.LowLabel}</output></p>
				<p><span>N2 / CORE</span><output data-render-output="n2">{data.visual.HighLabel}</output></p>
				<p><span>BURNER GAS</span><output data-render-output="temperature">{data.visual.TemperatureLabel}</output></p>
				<p class="render-caption" data-render-output="caption">{data.visual.Caption}</p>
			</div>
			<div class="render-operating-controls" data-render-controls>
				<label for="render-fuel">Fuel command <output data-render-fuel-label>2.0550 kg/s</output></label>
				<input
					id="render-fuel"
					data-render-fuel
					type="range"
					min="0.2054"
					max="2.055"
					step="0.0001"
					value="2.055"
					disabled
				 />
				<nav aria-label="Rendered ground point"><button type="button" data-render-lto="idle" disabled>Idle</button><button type="button" data-render-lto="approach" disabled>Approach</button><button type="button" data-render-lto="climbout" disabled>Climbout</button><button type="button" data-render-lto="takeoff" disabled>Takeoff</button></nav>
			</div>
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
		<footer class="colophon"><p>ANATOMY, WITH NUMBERS.</p><p>JT9D steady ground solver · Schematic geometry · Solved spools at 1/800 speed</p><a href="https://github.com/odvcencio/gosx/tree/main/examples/turbofan">Built with GoSX <span aria-hidden="true">↗</span></a></footer>
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
			props={data.browserProps}
			data-cycle-mode={data.mode}
		>
			<p class="cycle-loading">Loading interactive cycle controls. The initial calculated point is shown below.</p>
			<div class="workbench-grid">
				<fieldset class="cycle-inputs" disabled>
					<legend>Choose a model</legend>
					<nav class="cycle-modes" aria-label="Numerical model"><button type="button" data-cycle-model="generic" aria-pressed="true">Cycle design</button><button type="button" data-cycle-model="mapped" aria-pressed="false">JT9D maps</button></nav>
					<div class="cycle-presets" aria-label="Flight condition presets" data-design-only><button type="button" data-cycle-preset="static">Sea level</button><button type="button" data-cycle-preset="cruise">Cruise</button><button type="button" data-cycle-preset="reset">Reset all</button></div>
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
					<label for="cycle-t4" data-design-only><span>Burner exit temperature</span><output for="cycle-t4" data-cycle-input-label="turbineInletK">1600 K</output></label>
					<input
						id="cycle-t4"
						data-design-only
						name="turbineInletK"
						type="range"
						min="1000"
						max="1800"
						step="10"
						value={data.inputs.TurbineInletK}
					 />
					<label for="cycle-opr" data-design-only><span>Overall pressure ratio</span><output for="cycle-opr" data-cycle-input-label="overallPressureRatio">30 : 1</output></label>
					<input
						id="cycle-opr"
						data-design-only
						name="overallPressureRatio"
						type="range"
						min="5"
						max="40"
						step="1"
						value={data.inputs.OverallPressureRatio}
					 />
					<label for="cycle-fpr" data-design-only><span>Fan pressure ratio</span><output for="cycle-fpr" data-cycle-input-label="fanPressureRatio">1.50 : 1</output></label>
					<input
						id="cycle-fpr"
						data-design-only
						name="fanPressureRatio"
						type="range"
						min="1.1"
						max="1.8"
						step="0.01"
						value={data.inputs.FanPressureRatio}
					 />
					<label for="cycle-bypass" data-design-only><span>Bypass ratio</span><output for="cycle-bypass" data-cycle-input-label="bypassRatio">8.0 : 1</output></label>
					<input
						id="cycle-bypass"
						data-design-only
						name="bypassRatio"
						type="range"
						min="1"
						max="12"
						step="0.5"
						value={data.inputs.BypassRatio}
					 />
					<p class="input-note" data-design-only>Each change solves a new design point. It does not simulate throttling a fixed engine.</p>
					<div data-map-only><label for="cycle-fuel"><span>Fuel command</span><output for="cycle-fuel" data-cycle-input-label="fuelFlowKGS">2.0550 kg/s</output></label><input
							id="cycle-fuel"
							name="fuelFlowKGS"
							type="range"
							min="0.2054"
							max="2.055"
							step="0.0001"
							value="2.055"
						 />
						<div class="cycle-presets lto-presets" aria-label="ICAO ground points"><button type="button" data-cycle-lto="takeoff">Takeoff 100%</button><button type="button" data-cycle-lto="climbout">Climbout 85%</button><button type="button" data-cycle-lto="approach">Approach 30%</button><button type="button" data-cycle-lto="idle">Idle 7%</button></div>
						<p class="input-note">JT9D-7R4D / -7R4D1 · ICAO 1PW026. Fixed geometry; five component maps solve airflow, pressure ratios and spool speeds. Idle uses explicitly modeled low-speed map extensions.</p></div>
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
					<div class="shaft-powers"><p><span data-lp-label>Fan / LP shaft</span><strong data-cycle-output="fan-power">{data.cycle.Values["fan-power"]}</strong> MW</p><p><span>Compressor / HP shaft</span><strong data-cycle-output="compressor-power">{data.cycle.Values["compressor-power"]}</strong> MW</p></div>
				</div>
			</div>
			<div class="cycle-status-row"><p class="balance-status" data-cycle-output="status" role="status">{data.cycle.Values["status"]}</p><button type="button" class="cycle-export" data-cycle-export disabled>Export point · CSV <span aria-hidden="true">↗</span></button></div>
			<p class="validation-status mapped-row"><a href="#public-validation-title">Public speed check</a>: <output data-eng-key="public/status" data-eng-format="t">{data.engineering.Text["public/status"]}</output></p>
			<EngineeringInspector />
			<section class="mapped-details" data-map-only aria-label="JT9D mapped model">
				<dl class="mapped-metrics"><div><dt>Model N1</dt><dd><output data-cycle-output="n1">—</output> rpm</dd></div><div><dt>Model N2</dt><dd><output data-cycle-output="n2">—</output> rpm</dd></div><div><dt>Solved OPR</dt><dd><output data-cycle-output="mapped-opr">—</output> : 1</dd></div><div><dt>Solved bypass</dt><dd><output data-cycle-output="mapped-bypass">—</output> : 1</dd></div></dl>
				<div class="map-plot-grid"><Each of={data.mapPlots} as="plot"><figure class="component-map"><figcaption>{plot.Label}<span>SCALED NASA MAPS</span></figcaption><svg viewBox="0 0 360 210" role="img" aria-label={plot.Label}><path class="map-axis" d="M35 20V175H325" /><g class="map-speed-lines"><Each of={plot.Paths} as="path"><path d={path} /></Each></g><g class="map-extension-lines"><Each of={plot.ExtensionPaths} as="path"><path d={path} /></Each></g><circle
									class="map-operating-point"
									data-map-dot={plot.Name}
									cx="0"
									cy="0"
									r="4"
									visibility="hidden"
								 /><g class="map-labels"><text x="3" y="28">{plot.MaxPressure}</text><text x="19" y="178">1</text><text x="35" y="193">0</text><text x="295" y="193">{plot.MaxFlow}</text><text x="105" y="209">Corrected flow · kg/s</text></g></svg></figure></Each></div>
				<p class="map-note">Horizontal: corrected flow. Vertical: pressure ratio. Solid curves: scaled public map tables. Dashed curves: bounded modeled extensions. Orange marks the solved point. <strong data-cycle-output="map-region">—</strong>. Booster exit: <output data-cycle-output="booster-temperature">—</output> K · Cooled HPT inlet: <output data-cycle-output="hpt-inlet-temperature">—</output> K · Largest matching residual: <output data-cycle-output="match-residual">—</output>.</p>
				<details class="cycle-details calibration-details" open><summary>Calibration &amp; unfitted checks · sea-level static</summary><div class="station-scroll" tabindex="0" role="region" aria-label="Calibration comparison"><table class="station-table"><thead><tr><th scope="col">ICAO point</th><th scope="col">Fuel · kg/s</th><th scope="col">Target · kN</th><th scope="col">Model · kN</th><th scope="col">Error</th></tr></thead><tbody><Each of={data.mapChecks} as="check"><tr><th scope="row">{check.Mode}<span class="check-role">{check.Role}</span></th><td>{check.Fuel}</td><td>{check.Target}</td><td>{check.Prediction}</td><td>{check.Error}</td></tr></Each></tbody></table></div>
					<p class="map-note">Takeoff anchors the rated cycle; approach fits one shared low-speed fan/booster efficiency correction. Idle fits an additional correction below map speed 0.5 and uses modeled extensions. Climbout is excluded from fitting. These three fitted ground points do not validate flight conditions, station measurements, spool RPM or surge margins.</p>
					<p class="map-note">Sources: <a							href="https://github.com/nasa/T-MATS/tree/ad6e4d5d0d76c229db4eb72ca40ef58d5ddc4014/Trunk/TMATS_Examples/Example_JT9D/Simsetup">NASA T-MATS JT9D maps</a> and <a							href="https://www.easa.europa.eu/en/domains/environment/icao-aircraft-engine-emissions-databank">ICAO/EASA databank 03/2026</a>, UID 1PW026. The maps are scaled NASA model shapes, not proprietary manufacturer maps. Temperature-dependent NASA7 frozen-air properties, air-equivalent combustion products, 9% cooling bleed and prescribed component priors remain approximations. Rotor motion follows the estimated N1/N2 ratio at 1/800 speed. Color shows schematic gas temperature; metal temperature and CFD are not calculated.</p></details>
			</section>
			<details class="cycle-details" open>
				<summary>Station conditions &amp; nozzle sizing</summary>
				<div class="station-scroll" tabindex="0" role="region" aria-label="Scrollable station conditions"><table class="station-table"><caption>Total temperature and pressure, except nozzle exit rows which show static conditions.</caption><thead><tr><th scope="col">Station</th><th scope="col">T · K</th><th scope="col">P · kPa</th><th scope="col">Flow · kg/s</th></tr></thead><tbody>
							<Each of={data.cycle.Stations} as="station"><tr data-cycle-station={station.ID}><th scope="row">{station.Name}</th><td data-quantity="temperature">{station.T}</td><td data-quantity="pressure">{station.P}</td><td data-quantity="flow">{station.Flow}</td></tr></Each>
						</tbody></table></div>
				<div class="nozzle-grid"><p>Core nozzle <strong data-cycle-output="core-nozzle">{data.cycle.Values["core-nozzle"]}</strong><span>Exit area <output data-cycle-output="core-area">{data.cycle.Values["core-area"]}</output> m²</span></p><p>Bypass nozzle <strong data-cycle-output="bypass-nozzle">{data.cycle.Values["bypass-nozzle"]}</strong><span>Exit area <output data-cycle-output="bypass-area">{data.cycle.Values["bypass-area"]}</output> m²</span></p></div>
			</details>
			<details class="cycle-details assumptions">
				<summary>Model assumptions &amp; what these numbers mean</summary>
				<p data-design-only>A one-dimensional, steady design-point Brayton cycle with separate core and bypass streams. Both shafts balance turbine work against their loads. Ideal convergent nozzles include choking and pressure thrust; their areas resize to pass the prescribed flow at every point.</p>
				<p data-design-only>Constant γ = 1.4, R = 287.05 J/(kg·K), cp = 1004.675 J/(kg·K). ISA troposphere. Corrected core airflow = 40 kg/s at 288.15 K and 101.325 kPa, referenced to inlet total conditions. Fan/compressor/turbine efficiencies = 0.90/0.88/0.91; shaft and combustion efficiencies = 0.99. Inlet/burner pressure recovery = 0.99/0.95. Fuel LHV = 43 MJ/kg.</p>
				<p data-design-only>Useful for cycle trends and energy accounting. This generic model has no engine calibration, component maps, surge margin, cooling, variable gas properties, shaft inertia, or fixed-geometry off-design matching. It does not predict a real engine’s performance or the rotor RPM shown above.</p>
				<p>Equations: <a href="https://www.grc.nasa.gov/www/k-12/airplane/compress.html">NASA compressor</a>, <a href="https://www.grc.nasa.gov/www/k-12/airplane/burnth.html">burner</a>, <a href="https://www.grc.nasa.gov/www/k-12/airplane/powtrbth.html">turbine</a>, <a href="https://www.grc.nasa.gov/www/k-12/airplane/isentrop.html">isentropic flow</a>, and <a href="https://www1.grc.nasa.gov/beginners-guide-to-aeronautics/thrust-force/">thrust</a>. Largest relative conservation residual at this point: <output data-cycle-output="residual">{data.cycle.Values["residual"]}</output>.</p>
			</details>
		</Surface>
	</section>
}

func EngineeringInspector() Node {
	return <section class="engineering-inspector" aria-labelledby="engineering-title">
		<div class="engineering-heading"><div><p class="eyebrow">OPERATING-POINT ANALYSIS</p><h3 id="engineering-title">The engineering behind the point.</h3><p class="engineering-intro">Derived from the current solution in SI units, with shaft speeds in rpm. Component and station values are model estimates; public observations are identified in the calibration table.</p></div><div class="engineering-actions"><button type="button" data-engineering-expand disabled>Expand all tables</button><button type="button" data-engineering-collapse disabled>Collapse tables</button><button type="button" data-engineering-export disabled>Export engineering · CSV</button></div></div>
		<div id="engineering-overview">
			<div class="engineering-plots">
				<figure class="engineering-chart"><figcaption>Temperature–entropy path <span>AIR-EQUIVALENT GAS</span></figcaption><svg viewBox="0 0 480 220" role="img" aria-label="Core station temperature entropy path"><path class="engineering-grid" d="M48 30H448 M48 105H448 M48 180H448 M48 30V180" /><g class="engineering-axis-labels"><text x="3" y="34">2000 K</text><text x="3" y="109">1100</text><text x="10" y="184">200</text><text x="48" y="202" data-eng-key="plot/s-min.J_kgK" data-eng-scale="0.001" data-eng-digits="2">{data.engineeringAxes["smin"]}</text><text x="416" y="202" data-eng-key="plot/s-max.J_kgK" data-eng-scale="0.001" data-eng-digits="2">{data.engineeringAxes["smax"]}</text><text x="135" y="218">s · kJ/(kg·K), relative reference</text></g><path
							class="engineering-line"
							data-eng-key="plot/ts-path"
							data-eng-format="t"
							data-eng-attr="d"
							d={data.engineering.Text["plot/ts-path"]}
						 /><g
							class="ts-station-label"
							data-eng-key="plot/2/ts-transform"
							data-eng-format="t"
							data-eng-attr="transform"
							transform={data.engineering.Text["plot/2/ts-transform"]}
						><circle r="3" /><text x="6" y="-5">2</text></g><g
							class="ts-station-label"
							data-eng-key="plot/3/ts-transform"
							data-eng-format="t"
							data-eng-attr="transform"
							transform={data.engineering.Text["plot/3/ts-transform"]}
						><circle r="3" /><text x="6" y="-5">3</text></g><g
							class="ts-station-label"
							data-eng-key="plot/4/ts-transform"
							data-eng-format="t"
							data-eng-attr="transform"
							transform={data.engineering.Text["plot/4/ts-transform"]}
						><circle r="3" /><text x="6" y="-5">4</text></g><g
							class="ts-station-label"
							data-eng-key="plot/5/ts-transform"
							data-eng-format="t"
							data-eng-attr="transform"
							transform={data.engineering.Text["plot/5/ts-transform"]}
						><circle r="3" /><text x="6" y="-5">5</text></g></svg><p>Core station path through compression, heat addition, cooling and expansion. This is an open engine with changing mass, not a closed thermodynamic cycle. Reference: 288.15 K, 101.325 kPa.</p></figure>
				<figure class="engineering-chart"><figcaption>Pressure through the core <span>LOGARITHMIC PRESSURE RATIO</span></figcaption><svg
						viewBox="0 0 480 220"
						role="img"
						aria-label="Pressure ratio relative to ambient through core stations"
					><path class="engineering-grid" d="M48 30H448 M48 105H448 M48 180H448 M48 30V180" /><g class="engineering-axis-labels"><text x="8" y="34" data-eng-key="plot/p-max.1" data-eng-digits="0">{data.engineeringAxes["pmax"]}</text><text x="8" y="184">0.8</text><text x="48" y="202">2</text><text x="92" y="202">13</text><text x="136" y="202" class="mapped-row">25</text><text x="181" y="202">3</text><text x="226" y="202">4</text><text x="270" y="202" class="mapped-row">41</text><text x="310" y="202" class="mapped-row">45a</text><text x="357" y="202">45</text><text x="402" y="202">5</text><text x="445" y="202">9</text><text x="132" y="218">Station sequence · P / Pambient</text></g><path
							class="engineering-line pressure-line"
							data-eng-key="plot/pressure-path"
							data-eng-format="t"
							data-eng-attr="d"
							d={data.engineering.Text["plot/pressure-path"]}
						 /></svg><p>Station spacing is schematic. States are total except nozzle exit 9, which is static. The pressure drop across a turbine supplies work to its shaft.</p></figure>
			</div>
			<details class="engineering-sweep mapped-row" open><summary>Idle-to-takeoff model operating line · 81 solved points</summary><p class="engineering-note">Fixed geometry, sea-level static. These curves are solver predictions, with three calibration points; they are not an engine test sweep. N1 is the solid speed curve and N2 the dashed curve. Orange dots show the current solution.</p><p class="engineering-note">{data.sweepError}</p><div class="engineering-plots"><Each of={data.sweepCharts} as="chart"><figure class="engineering-chart"><figcaption>{chart.Label}<span>{chart.Unit}</span></figcaption><svg viewBox="0 0 480 220" role="img" aria-label={chart.Label}><path class="engineering-grid" d="M48 30H448 M48 105H448 M48 180H448 M48 30V180" /><g class="engineering-axis-labels"><text x="3" y="34">{chart.Maximum}</text><text x="28" y="184">0</text><text x="48" y="202">0.2054</text><text x="415" y="202">2.055</text><text x="190" y="218">Fuel · kg/s</text></g><path class="engineering-line" d={chart.Path} /><path class="engineering-line sweep-second" d={chart.SecondPath} /><circle
									class="map-operating-point"
									data-sweep-marker={chart.Key}
									data-sweep-max={chart.MaxY}
									cx={chart.X}
									cy={chart.Y}
									r="4"
								 /><circle
									class="map-operating-point sweep-second-dot"
									data-sweep-second={chart.Key}
									data-sweep-max={chart.MaxY}
									cx={chart.X}
									cy={chart.SecondY}
									r="4"
								 /></svg></figure></Each></div><div class="sampled-sensitivity"><p>Sampled thrust slope <strong data-eng-key="sensitivity/thrust.Ns_kg" data-eng-scale="0.001" data-eng-digits="2">—</strong> kN / (kg/s fuel)</p><p>Sampled N1 slope <strong data-eng-key="sensitivity/n1.rpm_s_kg" data-eng-digits="0">—</strong> rpm / (kg/s fuel)</p><p>Sampled N2 slope <strong data-eng-key="sensitivity/n2.rpm_s_kg" data-eng-digits="0">—</strong> rpm / (kg/s fuel)</p></div><p class="engineering-note" data-eng-key="sensitivity/bracket" data-eng-format="t">—</p><button class="cycle-export" type="button" data-engineering-sweep-export disabled>Export model sweep · CSV</button></details>
		</div>
		<div class="engineering-tables"><Each of={data.diagnosticTables} as="table"><details id={table.ID} class={"engineering-table " + table.Class} open><summary>{table.Title}</summary><div class="station-scroll" role="region" tabindex="0" aria-label={table.Title}><table class="station-table"><thead><tr><Each of={table.Headers} as="heading"><th scope="col">{heading}</th></Each></tr></thead><tbody><Each of={table.Rows} as="row"><tr class={row.Class}><th scope="row">{row.Label}</th><Each of={row.Cells} as="cell"><td><output
													data-eng-key={cell.Key}
													data-eng-format={cell.Format}
													data-eng-scale={cell.Scale}
													data-eng-digits={cell.Digits}
												>{cell.Value}</output></td></Each></tr></Each></tbody></table></div><p class="engineering-note">{table.Note}</p></details></Each></div>
		<p class="engineering-note mapped-row">Matching iterations (all continuation steps): <output data-eng-key="solver/iterations.1" data-eng-digits="0">—</output> · maximum normalized residual: <output data-eng-key="solver/max-residual.1" data-eng-format="e" data-eng-digits="2">—</output> · convergence threshold: 1e−9.</p>
		<aside class="engineering-provenance"><p><strong>Public observations</strong> Rated thrust, OPR, bypass ratio and ICAO fuel flows.</p><p><strong>Calibrated / assumed</strong> Scaled NASA maps, low-speed extensions, gas composition, cooling fractions, fixed nozzle areas and estimated spool RPM.</p><p><strong>Additional data needed</strong> Matched spool-speed/station measurements, variable geometry, surge margin, blade stress, tip Mach and transient inertia.</p></aside>
		<section class="public-validation mapped-row" aria-labelledby="public-validation-title">
			<p class="eyebrow">FROZEN MODEL · EXTERNAL REFERENCES</p>
			<h3 id="public-validation-title">Public RPM &amp; station checks</h3>
			<p class="validation-status" data-eng-key="public/status" data-eng-format="t">{data.engineering.Text["public/status"]}</p>
			<p class="engineering-note">The reported limits test speed compatibility. A value within a maximum does not validate operating RPM. The temperature and idle references below have applicability gaps; zero matched measured RPM/station points with complete conditions were identified. No model parameters were adjusted to these references.</p>
			<div class="station-scroll" role="region" tabindex="0" aria-label="Live public RPM limit checks"><table class="station-table"><thead><tr><th scope="col">Variant</th><th scope="col">Spool</th><th scope="col">Maximum · rpm</th><th scope="col">Current model · rpm</th><th scope="col">Model − limit · rpm</th><th scope="col">Finding</th></tr></thead><tbody><Each of={data.publicLimits} as="limit"><tr data-public-limit={limit.ID}><th scope="row">{limit.Label}</th><td>{limit.Rotor}</td><td>{limit.Limit}</td><td><output data-eng-key={"public/"+limit.ID+"/speed.rpm"} data-eng-digits="1">{limit.Model}</output></td><td><output data-eng-key={"public/"+limit.ID+"/difference.rpm"} data-eng-digits="1">{limit.Difference}</output></td><td><output data-eng-key={"public/"+limit.ID+"/status"} data-eng-format="t">{limit.Status}</output></td></tr></Each></tbody></table></div>
			<p class="engineering-note">FAA E3NE revision 16 (2019), Note 1, standard -7R4D/-7R4D1 variants. Converted (DM)/(EM) engines have separate limits and are not identified by the grouped ICAO entry. These limits are historical reference checks, not an operational approval.</p>
			<p class="engineering-note">Cockpit normalization from the -7R4D exhibit: 100% N1 = 3600 rpm; 100% N2 = 7807 rpm. Current model: <output data-eng-key="public/n1-gauge.1" data-eng-scale="100" data-eng-digits="2">—</output>% N1 / <output data-eng-key="public/n2-gauge.1" data-eng-scale="100" data-eng-digits="2">—</output>% N2. These cockpit references differ from the operating-line plot's rated-model normalization.</p>
			<p class="engineering-note">FAA Note 2 gives the takeoff (5-minute) total exhaust-temperature maximum as 625 C / 898.15 K for both standard variants. Source Tt7 immediately after the low-pressure turbine maps to solver station 5. Current model: <output data-eng-key="public/lpt-exhaust.K" data-eng-digits="2">—</output> K · <output data-eng-key="public/exhaust-status" data-eng-format="t">{data.engineering.Text["public/exhaust-status"]}</output>.</p>
			<Each of={data.publicGroups} as="group"><details class="validation-reference-group" open={group.Open}><summary>{group.Title}</summary><p class="engineering-note">{group.Note}</p><div class="station-scroll" role="region" tabindex="0" aria-label={group.Title}><table class="station-table"><thead><tr><th scope="col">Quantity</th><th scope="col">Published</th><th scope="col">Model</th><th scope="col">Difference</th><th scope="col">Applicability &amp; conditions</th></tr></thead><tbody><Each of={group.Rows} as="check"><tr data-public-comparison={check.ID} data-public-status={check.Status}><th scope="row">{check.Label}<span class="check-role"><a href={check.SourceURL}>{check.Source}</a></span></th><td>{check.Reference}</td><td>{check.Prediction}</td><td>{check.Difference}</td><td class="validation-context"><strong>{check.Status}</strong><p>{check.Conditions}</p><p>{check.Finding}</p></td></tr></Each></tbody></table></div></details></Each>
			<details class="validation-reference-group"><summary>Source provenance &amp; remaining validation gaps</summary><Each of={data.publicSources} as="source"><p class="engineering-note"><a href={source.URL}>{source.Title}</a> · {source.Locator}. {source.Kind}. {source.Notes}</p></Each><p class="engineering-note">NASA's source calls the combustor inlet Tt4; that location maps to this solver's station 3. Its takeoff SI and psia pressure entries disagree; the comparison retains the printed SI value and flags the discrepancy. NASA T-MATS is already a model prior and cannot independently validate its own scaled maps. Needed next: synchronized -7R4D/D1 fuel, ambient, bleed/load, measured spool speeds and gas-station readings with locations and uncertainty.</p></details>
		</section>
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
