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
			<p class="intro-copy">A machine that turns air into momentum.<br />Open the casing. Follow the flow.<br />See how every stage works together.</p>
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
		<section class="cycle" aria-labelledby="cycle-title">
			<div class="section-heading"><h2 id="cycle-title">One continuous cycle.</h2><p>AIR IN <span aria-hidden="true">────────→</span> THRUST OUT</p></div>
			<div class="stage-grid">
				<article class="stage"><span class="stage-number">01 <span>↗</span></span><h3>Fan &amp; bypass</h3><p>The fan accelerates air. Much of it travels around the core through the bypass duct, contributing thrust.</p><span class="stage-detail">MOVE MORE AIR</span></article>
				<article class="stage"><span class="stage-number">02 <span>→</span></span><h3>Compression</h3><p>Successive rotor and stator rows raise core pressure. Two concentric shafts let the spools turn at different speeds.</p><span class="stage-detail">RAISE THE PRESSURE</span></article>
				<article class="stage hot-stage"><span class="stage-number">03 <span>✳</span></span><h3>Combustion</h3><p>Fuel mixes with compressed air in an annular chamber. Burning adds energy before the flow reaches the turbines.</p><span class="stage-detail">ADD HEAT</span></article>
				<article class="stage"><span class="stage-number">04 <span>↗</span></span><h3>Turbine &amp; exhaust</h3><p>The turbines drive the compressor and fan. Remaining energy accelerates the exhaust through the core nozzle.</p><span class="stage-detail">EXTRACT &amp; ACCELERATE</span></article>
			</div>
		</section>
		<footer class="colophon"><p>ANATOMY, IN MOTION.</p><p>Illustrative geometry · Rotation slowed for study · Not an engineering simulation</p><a href="https://github.com/odvcencio/gosx/tree/main/examples/turbofan">Built with GoSX <span aria-hidden="true">↗</span></a></footer>
	</main>
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
