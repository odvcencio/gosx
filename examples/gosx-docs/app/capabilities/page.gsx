package docs

func Page() Node {
	return <article class="capabilities-page" aria-labelledby="capabilities-title">
		<header class="capabilities-header">
			<p class="capabilities-eyebrow">Scene3D capabilities</p>
			<h1 id="capabilities-title">What each renderer can draw</h1>
			<p>
				GoSX computes a backend verdict per scene in Go. The browser obeys it. A scene never silently drops a feature.
			</p>
			<p>
				Rows come from Matrix and LightKindFeatures. Each backend cell links to its renderer implementation.
			</p>
			<p>
				<a href="https://github.com/odvcencio/gosx/blob/main/scene/capability/capability.go">
					Read the Go capability matrix and light-kind mapping
				</a>
			</p>
			<p>
				<a href="/performance/">Read measured performance receipts</a>
			</p>
		</header>
		<section class="capabilities-browser" data-gosx-scene3d-status-scope aria-label="Browser backend probe">
			<div class="capabilities-browser__mark" aria-hidden="true">
				<Scene3D class="capabilities-browser__scene" {...data.probe} stats={false} />
			</div>
			<div>
				<h2>Your browser</h2>
				<p>
					The runtime selected
					<output data-gosx-scene3d-status="renderer">Waiting for the Scene3D runtime</output>
					.
				</p>
				<p class="capabilities-browser__note">
					The feature column below applies this selection to each row.
				</p>
			</div>
		</section>
		<div
			class="capabilities-table-wrap"
			role="region"
			aria-label="Scene3D feature support by renderer"
			tabindex="0"
		>
			<table class="capabilities-table">
				<caption>
					“Feature missing” means this renderer does not implement that feature. Optional gaps can degrade a scene; they do not automatically reject its backend.
				</caption>
				<thead>
					<tr>
						<th scope="col">Feature</th>
						<th scope="col">WebGPU</th>
						<th scope="col">WebGL2</th>
						<th scope="col">Canvas2D</th>
						<th scope="col">Your browser</th>
					</tr>
				</thead>
				<tbody>
					<Each of={data.rows} as="row">
						<tr data-feature={row.Feature}>
							<th scope="row">
								<span>{row.Label}</span>
								<small>{row.LightKinds}</small>
							</th>
							<td data-backend="webgpu" data-supported={row.WebGPU.Supported}>
								<strong>{row.WebGPU.Status}</strong>
								<p>{row.WebGPU.Reason}</p>
								<a href={row.WebGPU.SourceURL} target="_blank" rel="noopener noreferrer">Renderer source</a>
							</td>
							<td data-backend="webgl" data-supported={row.WebGL2.Supported}>
								<strong>{row.WebGL2.Status}</strong>
								<p>{row.WebGL2.Reason}</p>
								<a href={row.WebGL2.SourceURL} target="_blank" rel="noopener noreferrer">Renderer source</a>
							</td>
							<td data-backend="canvas2d" data-supported={row.Canvas2D.Supported}>
								<strong>{row.Canvas2D.Status}</strong>
								<p>{row.Canvas2D.Reason}</p>
								<a href={row.Canvas2D.SourceURL} target="_blank" rel="noopener noreferrer">Renderer source</a>
							</td>
							<td class="capabilities-browser-cell">
								<span class="capabilities-browser-cell__waiting">Waiting for browser selection.</span>
								<span class="capabilities-browser-cell__answer capabilities-browser-cell__answer--webgpu">
									<strong>
										{row.WebGPU.BackendLabel}
										:
										{row.WebGPU.Status}
									</strong>
									{row.WebGPU.Reason}
								</span>
								<span class="capabilities-browser-cell__answer capabilities-browser-cell__answer--webgl">
									<strong>
										{row.WebGL2.BackendLabel}
										:
										{row.WebGL2.Status}
									</strong>
									{row.WebGL2.Reason}
								</span>
								<span class="capabilities-browser-cell__answer capabilities-browser-cell__answer--canvas2d">
									<strong>
										{row.Canvas2D.BackendLabel}
										:
										{row.Canvas2D.Status}
									</strong>
									{row.Canvas2D.Reason}
								</span>
								<span class="capabilities-browser-cell__answer capabilities-browser-cell__answer--unsupported">
									<strong>No supported renderer</strong>
									The runtime could not start WebGPU, WebGL2, or Canvas2D in this browser.
								</span>
							</td>
						</tr>
					</Each>
				</tbody>
			</table>
		</div>
	</article>
}
