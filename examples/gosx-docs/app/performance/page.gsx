package performance

func Page() Node {
	return <section class="performance-page" aria-labelledby="performance-title">
		<header class="performance-header">
			<If cond={data.hasMeasurements}>
				<p class="performance-kicker">{data.measuredLabel}</p>
			</If>
			<h1 id="performance-title">Performance receipts</h1>
			<p class="performance-lede">
				Page scores, renderer timings, asset sizes, and the install quickstart, measured on a local production build.
			</p>
			<If cond={!data.hasMeasurements}>
				<p class="performance-pending" role="status">
					The measurement run has not been generated yet.
				</p>
			</If>
		</header>
		<If cond={data.hasMeasurements}>
			<section class="performance-section" aria-labelledby="performance-lighthouse-title">
				<div class="performance-section__heading">
					<div>
						<p class="performance-kicker">Mobile · three cold runs</p>
						<h2 id="performance-lighthouse-title">Page scores</h2>
					</div>
					<p>{data.lighthouseDescriptor}</p>
				</div>
				<div class="performance-table-wrap" tabindex="0">
					<table class="performance-table">
						<thead>
							<tr>
								<th scope="col">Page</th>
								<th scope="col">Performance</th>
								<th scope="col">Accessibility</th>
								<th scope="col">Best practices</th>
								<th scope="col">SEO</th>
								<th scope="col">CLS</th>
							</tr>
						</thead>
						<tbody>
							<Each of={data.lighthousePages} as="page">
								<tr data-lighthouse-path={page.Path}>
									<th scope="row">
										<a href={page.Path}>{page.Path}</a>
									</th>
									<td>
										<strong>{page.Median.Performance}</strong>
										<If cond={page.PerformanceTarget != ""}>
											<small class="performance-target">
												Target
												{page.PerformanceTarget}
												·
												{page.PerformanceStatus}
											</small>
										</If>
									</td>
									<td>
										<strong>{page.Median.Accessibility}</strong>
										<If cond={page.AccessibilityTarget != ""}>
											<small class="performance-target">
												Target
												{page.AccessibilityTarget}
												·
												{page.AccessibilityStatus}
											</small>
										</If>
									</td>
									<td>
										<strong>{page.Median.BestPractices}</strong>
										<If cond={page.BestPracticesTarget != ""}>
											<small class="performance-target">
												Target
												{page.BestPracticesTarget}
												·
												{page.BestPracticesStatus}
											</small>
										</If>
									</td>
									<td>
										<strong>{page.Median.SEO}</strong>
										<If cond={page.SEOTarget != ""}>
											<small class="performance-target">
												Target
												{page.SEOTarget}
												·
												{page.SEOStatus}
											</small>
										</If>
									</td>
									<td>
										<strong>{page.Median.CLS}</strong>
										<If cond={page.CLSTarget != ""}>
											<small class="performance-target">
												Target
												{page.CLSTarget}
												·
												{page.CLSStatus}
											</small>
										</If>
									</td>
								</tr>
							</Each>
						</tbody>
					</table>
				</div>
				<p class="performance-note">
					Each cell is the median of three cold Lighthouse runs. The individual run scores are included in the
					<a					href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/performance/receipts.json">receipt JSON</a>
					.
				</p>
			</section>
			<section class="performance-section" aria-labelledby="performance-gpu-title">
				<div class="performance-section__heading">
					<div>
						<p class="performance-kicker">Renderer timing</p>
						<h2 id="performance-gpu-title">3D frame times</h2>
					</div>
					<p>{data.gpuDescriptor}</p>
				</div>
				<Each of={data.receipts.GPU.Scenes} as="scene">
					<article class="performance-scene" data-demo-path={scene.Path}>
						<h3>
							<a href={scene.Path}>{scene.Title}</a>
						</h3>
						<div class="performance-table-wrap" tabindex="0">
							<table class="performance-table performance-table--compact">
								<thead>
									<tr>
										<th scope="col">Backend</th>
										<th scope="col">First draw</th>
										<th scope="col">Draw cadence p50</th>
										<th scope="col">Draw cadence p95</th>
										<th scope="col">rAF CPU p95</th>
									</tr>
								</thead>
								<tbody>
									<Each of={scene.Backends} as="backend">
										<tr>
											<th scope="row">{backend.Backend}</th>
											<td>
												{millisecondsLabel(backend.FirstDrawMs)}
											</td>
											<td>
												{millisecondsLabel(backend.CadenceP50Ms)}
											</td>
											<td>
												{millisecondsLabel(backend.CadenceP95Ms)}
											</td>
											<td>
												{millisecondsLabel(backend.RafCPUP95Ms)}
											</td>
										</tr>
									</Each>
								</tbody>
							</table>
						</div>
					</article>
				</Each>
			</section>
			<section class="performance-section" aria-labelledby="performance-bundles-title">
				<div class="performance-section__heading">
					<div>
						<p class="performance-kicker">Production build</p>
						<h2 id="performance-bundles-title">Client and runtime sizes</h2>
					</div>
					<p>{data.bundleDescriptor}</p>
				</div>
				<div class="performance-table-wrap" tabindex="0">
					<table class="performance-table">
						<thead>
							<tr>
								<th scope="col">Asset</th>
								<th scope="col">Type</th>
								<th scope="col">Raw bytes</th>
								<th scope="col">Brotli bytes</th>
							</tr>
						</thead>
						<tbody>
							<Each of={data.receipts.Bundles} as="bundle">
								<tr>
									<th scope="row">
										<code>{bundle.Path}</code>
									</th>
									<td>{bundle.Kind}</td>
									<td>{bundle.RawBytes}</td>
									<td>{bundle.BrotliBytes}</td>
								</tr>
							</Each>
						</tbody>
					</table>
				</div>
			</section>
			<section
				class="performance-section performance-quickstart"
				aria-labelledby="performance-quickstart-title"
			>
				<div>
					<p class="performance-kicker">Install timing</p>
					<h2 id="performance-quickstart-title">Quickstart</h2>
				</div>
				<p>
					<code>{data.receipts.Quickstart.Command}</code>
				</p>
				<dl>
					<div>
						<dt>Cold</dt>
						<dd>
							{secondsLabel(data.receipts.Quickstart.ColdSec)}
						</dd>
					</div>
					<div>
						<dt>Warm</dt>
						<dd>
							{secondsLabel(data.receipts.Quickstart.WarmSec)}
						</dd>
					</div>
				</dl>
				<p>{data.quickstartDescriptor}</p>
			</section>
			<footer class="performance-provenance">
				<p>
					{data.provenanceLabel}
					<code>{data.receipts.Machine.LoadAverage}</code>
					.
				</p>
				<p>{data.machineDescription}</p>
				<p>
					Receipts were measured on this source commit before the receipt data was recorded.
				</p>
				<p>
					<code>{data.receipts.Commit}</code>
				</p>
				<If cond={data.receipts.Tree != ""}>
					<p>Source tree</p>
					<p>
						<code>{data.receipts.Tree}</code>
					</p>
				</If>
				<ul class="performance-links" aria-label="Measurement sources">
					<li>
						<a						href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/performance/receipts.json">Committed receipts JSON</a>
					</li>
					<li>
						<a href="https://github.com/odvcencio/gosx/blob/main/scripts/showcase-receipts.sh">Measurement script</a>
					</li>
				</ul>
			</footer>
		</If>
	</section>
}
