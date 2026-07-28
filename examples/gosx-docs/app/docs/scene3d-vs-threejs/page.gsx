package docs

func Page() Node {
	return <div>
		<section id="summary">
			<h2>Summary</h2>
			<p>
				three.js is a browser rendering library. Scene3D is the 3D layer of a Go web framework. That difference explains most of what follows, in both directions.
			</p>
			<p>
				If you want the short answer: three.js covers far more of 3D rendering than Scene3D does, and Scene3D covers things three.js structurally cannot. Scene3D also costs more bytes per rendering feature. All three statements are true at once.
			</p>
			<div class="vs-stat-row">
				{StatCard("~90%", "of Scene3D features have a three.js counterpart")}
				{StatCard("~35%", "of the three.js surface is covered by Scene3D")}
				{StatCard("313 KB", "gzip, a Chromium Scene3D page")}
				{StatCard("~170 KB", "gzip, three.js core for a comparable app")}
			</div>
		</section>
		<section id="overlap">
			<h2>The Overlap Is Asymmetric</h2>
			<p>
				Overlap is not one number. Measured in each direction it gives two very different answers.
			</p>
			<ul>
				<li>
					Roughly
					<strong>90%</strong>
					of what Scene3D renders has a direct three.js counterpart. Meshes, PBR materials, lights, shadows, instancing, glTF, points, and post effects all exist on both sides.
				</li>
				<li>
					Roughly
					<strong>35%</strong>
					of the three.js surface is covered by Scene3D. three.js carries about 40 loaders, about 25 post-processing passes, WebXR, morph targets, and a decade of accumulated device workarounds. Scene3D carries one loader, eight post effects, and no WebXR.
				</li>
			</ul>
			<p>
				Read that as a scope statement, not a quality statement. Scene3D chose a narrow, complete slice. three.js chose breadth.
			</p>
		</section>
		<section id="coverage-table">
			<h2>Coverage by Area</h2>
			<p>
				Each figure below answers one question: how much of the three.js surface in that area does Scene3D reach?
			</p>
			<div class="vs-table-wrap">
				<table class="vs-matrix">
					<caption>
						Scene3D coverage of the three.js surface, by area.
					</caption>
					<thead>
						<tr>
							<th scope="col">Area</th>
							<th scope="col">Coverage</th>
							<th scope="col">What is missing</th>
						</tr>
					</thead>
					<tbody>
						<tr>
							<th scope="row">Lights</th>
							<td>100% type parity</td>
							<td>
								Nothing by type. Rect-area specular approximates, and a probe folds to ambient.
							</td>
						</tr>
						<tr>
							<th scope="row">Instancing</th>
							<td>~70%</td>
							<td>
								No batched multi-geometry draw. Scene3D adds GPU instance culling that three.js lacks.
							</td>
						</tr>
						<tr>
							<th scope="row">Materials</th>
							<td>~60%</td>
							<td>
								No node material graph. Fewer built-in material classes.
							</td>
						</tr>
						<tr>
							<th scope="row">Geometry</th>
							<td>~55%</td>
							<td>
								No extrude, lathe, tube, text, or shape geometry. No constructive solid geometry.
							</td>
						</tr>
						<tr>
							<th scope="row">Animation</th>
							<td>~50%</td>
							<td>
								No morph targets. No blend tree. No inverse kinematics.
							</td>
						</tr>
						<tr>
							<th scope="row">Helpers</th>
							<td>~50%</td>
							<td>
								No camera, light, arrow, or plane helpers.
							</td>
						</tr>
						<tr>
							<th scope="row">Raycasting</th>
							<td>~40%</td>
							<td>
								A glTF model hits a bounds box, not triangles. No layer masks.
							</td>
						</tr>
						<tr>
							<th scope="row">Post-processing</th>
							<td>~30%</td>
							<td>
								8 effects against roughly 25. No screen-space reflection, no temporal anti-aliasing, no outline pass.
							</td>
						</tr>
						<tr>
							<th scope="row">Loaders</th>
							<td>~3%</td>
							<td>
								1 loader against roughly 40. glTF only, and no compressed geometry.
							</td>
						</tr>
						<tr>
							<th scope="row">WebXR</th>
							<td>0%</td>
							<td>Everything. There is no XR path at all.</td>
						</tr>
					</tbody>
				</table>
			</div>
		</section>
		<section id="bytes">
			<h2>The Byte Comparison</h2>
			<p>
				A Chromium Scene3D page now ships about
				<strong>313 KB gzip</strong>
				. That covers the runtime, the scene core, the WebGPU backend, the glTF loader, animation, and text layout. The WebGL backend is a lazily fetched chunk. A WebGPU-capable browser skips it and saves 151,301 raw bytes, 41,829 gzip bytes, and 33,803 brotli bytes.
			</p>
			<p>
				three.js core is roughly
				<strong>170 KB gzip</strong>
				for a comparable application, before you add a loader, a control scheme, and a post-processing chain.
			</p>
			<p>
				So Scene3D still costs more per rendering feature. Do not read the lazy WebGL chunk as leanness. It removed a regression; it did not win the comparison.
			</p>
			<p>
				Two honest qualifiers, both of which cut in Scene3D's favour without closing the gap.
			</p>
			<ul>
				<li>
					The Scene3D figure is a whole page runtime, not a rendering library. It includes routing, islands, signals, hubs, forms, and text layout. A three.js application adds its own framework on top of the 170 KB.
				</li>
				<li>
					Scene declaration itself ships no JavaScript. The scene is data on the wire, so a bigger scene does not grow the bundle. In a three.js application, scene construction is code.
				</li>
			</ul>
			<p>
				If your only goal is the smallest possible bundle for one 3D canvas on an otherwise static page, three.js wins that comparison. Pick it.
			</p>
		</section>
		<section id="gosx-leads">
			<h2>Where Scene3D Leads</h2>
			<p>
				Seven capabilities have no three.js equivalent. Each one exists because Scene3D is a framework layer rather than a browser library.
			</p>
			<ul>
				<li>
					<strong>CSS-driven scene state.</strong>
					A material, light, environment, points, post-effect, or compute-particle field can read a CSS custom property. A class change or a media query then drives scene state. The planner interpolates from the old value to the new one, using the transition timing it parses from the record. No authored JavaScript animates anything.
				</li>
				<li>
					<strong>GPU water simulation.</strong>
					Five feedback compute kernels drive a heightfield with caustics, reflection, refraction, and floating-object displacement, on WebGPU and on WebGL2. In three.js this is an example you copy, not a declarative node.
				</li>
				<li>
					<strong>First-class compute particles.</strong>
					<span class="inline-code">scene.ComputeParticles</span>
					is a node with a declarative emitter and force list, backed by a WebGPU compute kernel you can replace.
				</li>
				<li>
					<strong>GPU memory caps by default.</strong>
					Post-effect pixels, shadow-map pixels, and HTML texture pixels each carry a safe default cap and an explicit opt-out. three.js allocates what you ask for.
				</li>
				<li>
					<strong>Server-computed capability verdicts.</strong>
					Go decides which backend can render a scene faithfully, ships the verdict, and the client obeys it. A scene a backend would draw wrong gets diverted instead.
				</li>
				<li>
					<strong>
						A serializable, diffable scene representation.
					</strong>
					The scene is data. You can diff two states into a minimal command list, stream it over a hub, validate it against a schema, and inspect it in tooling.
				</li>
				<li>
					<strong>
						GPU-free deterministic preview and golden hashes.
					</strong>
					<span class="inline-code">scene/preview</span>
					rasterizes a frame in pure Go with no driver.
					<span class="inline-code">scene/harness</span>
					records a SHA-256 hash per frame plus coverage, edge energy, draw counts, and shader artifact hashes. You can assert on a rendered image in continuous integration with no browser and no GPU.
				</li>
			</ul>
		</section>
		<section id="threejs-leads">
			<h2>
				Where three.js Leads, and It Is Not Close
			</h2>
			<p>
				Five areas are not a fair fight. If your project needs one of them, use three.js.
			</p>
			<ul>
				<li>
					<strong>Loader breadth.</strong>
					three.js reads roughly 40 formats. Scene3D reads glTF 2.0 and nothing else. Scene3D also cannot decode Draco or meshopt geometry, and cannot transcode KTX2 or Basis textures. It raises a named error rather than build a broken mesh, which is honest but is still a hard stop.
				</li>
				<li>
					<strong>Post-processing depth.</strong>
					Roughly 25 passes against 8. Screen-space reflection, temporal anti-aliasing, outline, glitch, motion blur, and much more exist there and not here.
				</li>
				<li>
					<strong>WebXR.</strong>
					three.js has a mature XR path with controllers, hand tracking, and layers. Scene3D has none.
				</li>
				<li>
					<strong>Morph-target character animation.</strong>
					No morph targets and no blend tree means facial animation and modern character blending are out of reach. Cross-fade is the ceiling.
				</li>
				<li>
					<strong>Device-quirk maturity and ecosystem.</strong>
					A decade of driver workarounds, plus react-three-fiber, drei, Rapier bindings, and thousands of examples. Scene3D has none of that mass, and no amount of design compensates for it.
				</li>
			</ul>
		</section>
		<section id="choosing">
			<h2>Choosing Between Them</h2>
			<h3>Choose three.js when</h3>
			<ul>
				<li>
					You must load a format that is not glTF, or an asset that uses Draco, meshopt, or KTX2.
				</li>
				<li>You need WebXR.</li>
				<li>
					You need morph targets, a blend tree, or inverse kinematics.
				</li>
				<li>
					You need a specific post-processing pass Scene3D does not have.
				</li>
				<li>
					Your team already knows three.js and the ecosystem is the point.
				</li>
				<li>
					The 3D canvas is the only dynamic part of an otherwise static page, and bundle size decides.
				</li>
			</ul>
			<h3>Choose Scene3D when</h3>
			<ul>
				<li>
					Your server already speaks Go and you want one language across the stack.
				</li>
				<li>
					The scene state comes from server data and should change without shipping scene code.
				</li>
				<li>
					You want scene state to follow the theme, a media query, or a class change through CSS.
				</li>
				<li>
					You need deterministic rendering tests with no browser and no GPU.
				</li>
				<li>
					You want the framework to cap GPU memory and pick a backend honestly by default.
				</li>
				<li>
					Your page is already a GoSX application with routing, islands, hubs, and forms.
				</li>
			</ul>
			<h3>Neither is a drop-in for the other</h3>
			<p>
				Do not plan a mechanical port in either direction. Two differences break line-by-line translation.
			</p>
			<ul>
				<li>
					<strong>No hierarchical scale.</strong>
					A GoSX world transform carries position and rotation only.
					<span class="inline-code">scene.Group</span>
					has no scale field, and parent scale never propagates. Every
					<span class="inline-code">group.scale.set()</span>
					call needs a redesign, not a rename. Read
					<a href="/docs/scene3d#no-hierarchical-scale" data-gosx-link="true">No Hierarchical Scale</a>
					before you start.
				</li>
				<li>
					<strong>Declaration, not construction.</strong>
					A three.js scene is imperative code that mutates objects. A Scene3D scene is a value the server produces each render. Behaviour that a three.js app writes as a per-frame callback becomes a declarative field, a signal binding, a transition, or a diffed command list.
				</li>
			</ul>
		</section>
	</div>
}
