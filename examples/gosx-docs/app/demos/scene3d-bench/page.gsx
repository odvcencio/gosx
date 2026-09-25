package docs

func Page() Node {
	return <section class="scene3d-bench" aria-label="Scene3D render benchmark">
		<header class="scene3d-bench__heading">
			<p>Diagnostics / Scene3D</p>
			<h1>Renderer Bench</h1>
			<span>
				Switch workloads and watch render time and frame cadence change.
			</span>
		</header>
		<div class="scene3d-bench__workspace">
			<div class="scene3d-bench__scene" aria-label="Live 3D workload">
				<Scene3D {...data.scene} />
			</div>
			<div class="scene3d-bench__overlay" id="bench3d-overlay" data-workload={data.workload}>
				<h2 class="scene3d-bench__title">Live measurements</h2>
				<nav class="scene3d-bench__workloads" aria-label="Switch workload">
					<a href="?workload=static">Static</a>
					<a href="?workload=pbr-heavy">PBR heavy</a>
					<a href="?workload=thick-lines">Thick lines</a>
					<a href="?workload=particles">Particles</a>
					<a href="?workload=mesh-swarm">Mesh swarm</a>
					<a href="?workload=particles-storm">Particle storm</a>
					<a href="?workload=mixed">Mixed</a>
				</nav>
				<div class="scene3d-bench__row">
					<span>active workload</span>
					<b id="bench3d-workload">{data.workload}</b>
				</div>
				<div class="scene3d-bench__row scene3d-bench__row--prev" id="bench3d-prev-row" hidden>
					<span>prev workload</span>
					<b id="bench3d-prev">—</b>
				</div>
				<div class="scene3d-bench__row">
					<span>CPU submit · current</span>
					<b id="bench3d-current">—</b>
				</div>
				<div class="scene3d-bench__row">
					<span>CPU submit · mean</span>
					<b id="bench3d-mean">—</b>
				</div>
				<div class="scene3d-bench__row">
					<span>CPU submit · p50</span>
					<b id="bench3d-p50">—</b>
				</div>
				<div class="scene3d-bench__row">
					<span>CPU submit · p95</span>
					<b id="bench3d-p95">—</b>
				</div>
				<div class="scene3d-bench__row">
					<span>CPU submit · max</span>
					<b id="bench3d-max">—</b>
				</div>
				<div class="scene3d-bench__row">
					<span>CPU samples</span>
					<b id="bench3d-samples">0</b>
				</div>
				<div class="scene3d-bench__row">
					<span>rAF cadence</span>
					<b id="bench3d-fps">—</b>
				</div>
				<div class="scene3d-bench__row">
					<span>rAF p95</span>
					<b id="bench3d-raf-p95">—</b>
				</div>
				<svg
					class="scene3d-bench__histogram"
					id="bench3d-histogram"
					viewBox="0 0 240 40"
					width="100%"
					height="40"
					aria-hidden="true"
				></svg>
				<p class="scene3d-bench__legend">
					Green is under 10 ms, amber is 10–16.7 ms, and red is over 16.7 ms. CPU submit is one part of a frame.
				</p>
				<div class="scene3d-bench__subhead">rAF fps · last ~10s</div>
				<svg
					class="scene3d-bench__sparkline"
					id="bench3d-fps-sparkline"
					viewBox="0 0 240 32"
					width="100%"
					height="32"
					aria-hidden="true"
				></svg>
				<div class="scene3d-bench__actions">
					<button type="button" id="bench3d-reset">Reset</button>
					<button type="button" id="bench3d-copy">Copy JSON</button>
					<button type="button" id="bench3d-download">Download</button>
					<span id="bench3d-action-status" class="scene3d-bench__action-status" role="status"></span>
				</div>
				<div class="scene3d-bench__gpu" id="bench3d-gpu-strip">
					<div>
						<span>GPU</span>
						<b id="bench3d-gpu">detecting…</b>
					</div>
					<div>
						<span>API</span>
						<b id="bench3d-api">—</b>
					</div>
				</div>
				<details class="scene3d-bench__method">
					<summary>How the measurements work</summary>
					<p class="scene3d-bench__note">
						CPU submit measures the synchronous Scene3D render call. It does not include GPU completion. Frame cadence includes work elsewhere in the browser and machine. Previous workload is a snapshot from this tab, stored for comparison.
					</p>
				</details>
			</div>
		</div>
		<script src="/scene3d-bench-client.js"></script>
	</section>
}
