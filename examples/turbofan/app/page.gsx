package app

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
		<section class="viewer" aria-label="Interactive turbofan model" data-gosx-scene3d-status-scope>
			<div class="viewer-heading"><div><span class="model-id">TF—01</span><span class="model-description">TWO-SPOOL / HIGH-BYPASS</span></div><span class="motion-state"><span class="status-dot"></span>{data.state}</span></div>
			<div class="scene-area"><Scene3D {...data.scene} stats={false} /></div>
			<div class="viewer-controls">
				<nav class="segmented" aria-label="Casing view"><a href={data.cutawayURL} data-active={data.cutawayActive} aria-current={data.cutawayActive}>Cutaway</a><a href={data.exteriorURL} data-active={data.exteriorActive} aria-current={data.exteriorActive}>Exterior</a></nav>
				<nav class="camera-controls" aria-label="Camera presets"><a
						href={data.perspectiveURL}
						data-active={data.perspectiveActive}
						aria-current={data.perspectiveActive}
					>Perspective</a><a href={data.sideURL} data-active={data.sideActive} aria-current={data.sideActive}>Side</a><a href={data.intakeURL} data-active={data.intakeActive} aria-current={data.intakeActive}>Intake</a></nav>
				<a class="motion-control" href={data.motionURL}>{data.motionLabel}<span aria-hidden="true"> ↗</span></a>
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
