package docs

func ZooTraitCard(props any) Node {
	return <article class="card zoo-trait">
		<strong>{props.Kicker}</strong>
		<h3>{props.Title}</h3>
		<p>{props.Body}</p>
	</article>
}

func Page() Node {
	return <article class="zoo-shell">
		<section class="page-topper">
			<span class="eyebrow">Scene3D Demo</span>
			<p class="lede">
				Boxes, spheres, pyramids, and planes share one runtime path and stay inside the routed GoSX shell.
			</p>
		</section>
		<section class="zoo-hero">
			<div class="zoo-hero__copy">
				<h1>
					Geometry Zoo is a native 3D route, not a detached client app.
				</h1>
				<p>
					The page shell still belongs to the server, but the canvas is genuinely interactive. Move across the surface, then use the arrow keys to bend camera depth, wireframes, and palette bias in real time.
				</p>
				<div class="hero-actions">
					<Link class="hero-link primary" href="/docs/runtime">Read the runtime docs</Link>
					<Link class="hero-link" href="/demos/cms">Back to the CMS demo</Link>
				</div>
			</div>
			<div class="zoo-stage">
				<Scene3D class="zoo-scene" {...data.scene}>
					<div class="scene-fallback">Loading the Geometry Zoo runtime...</div>
				</Scene3D>
				<div class="zoo-legend">
					<div class="zoo-control">
						<kbd>Pointer</kbd>
						<p>
							Steer the camera and offset the forms directly on the canvas.
						</p>
					</div>
					<div class="zoo-control">
						<div class="scene-keyset">
							<kbd>Left</kbd>
							<kbd>Right</kbd>
							<kbd>Up</kbd>
						</div>
						<p>
							Shift palette, enable wireframe, and tighten the lens without leaving the route.
						</p>
					</div>
				</div>
			</div>
		</section>
		<section class="feature-grid">
			<Each as="trait" of={data.traits}>
				<ZooTraitCard {...trait}></ZooTraitCard>
			</Each>
		</section>
		<section class="callout">
			<strong>Why this route matters</strong>
			<p>
				The Scene3D canvas uses the same file routing, metadata, navigation, and export pipeline as the docs pages. Interactivity is not a carve-out from the app model.
			</p>
		</section>
	</article>
}
