package docs

func Page() Node {
	return <section
		class="scene3d-showcase"
		aria-label="Scene3D cinematic PBR showcase — interactive 3D demo"
		role="region"
	>
		<div class="scene3d-showcase__canvas">
			<Scene3D {...data.scene} />
		</div>
		<div class="scene3d-showcase__overlay">
			<p class="scene3d-showcase__eyebrow">Typed Go → SceneIR → browser GPU</p>
			<h1 class="scene3d-showcase__title">Scene3D</h1>
			<p class="scene3d-showcase__tagline">
				Seven materials, four lights, slow per-object spin, a glossy clearcoat floor, shadows, ACES tonemapping, bloom, and orbit controls—declared in Go.
			</p>
			<p class="scene3d-showcase__runtime" aria-live="polite">
				<span>GoSX renderer</span>
				<output id="scene3d-showcase-backend">starting…</output>
			</p>
			<p class="scene3d-showcase__controls">
				Drag to orbit · scroll or pinch to zoom
			</p>
			<details class="scene3d-showcase__proof">
				<summary>What GoSX owns</summary>
				<ul>
					<li>Typed scene graph, stable mesh IDs, and per-mesh declarative spin</li>
					<li>
						PBR materials and a three-point lighting rig
					</li>
					<li>
						Shadow, bloom, vignette, color grade, and ACES passes
					</li>
					<li>
						Responsive orbit interaction and backend fallback
					</li>
				</ul>
				<a
					href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/demos/scene3d/program.go"
					target="_blank"
					rel="noopener noreferrer"
				>View the typed scene source</a>
			</details>
		</div>
		{Scene3DShowcaseProofScript()}
	</section>
}

func Scene3DShowcaseProofScript() Node {
	return <script>
		{`
	(function() {
	  function title(value) {
	    if (!value) return "starting…";
	    if (value === "webgpu") return "WebGPU";
	    if (value === "webgl" || value === "webgl2") return "WebGL2";
	    return value.replace(/(^|-)([a-z])/g, function(_, dash, letter) { return (dash ? " " : "") + letter.toUpperCase(); });
	  }
	  function sync() {
	    var output = document.getElementById("scene3d-showcase-backend");
	    var mount = document.querySelector(".scene3d-showcase [data-gosx-scene3d-renderer]");
	    if (!output || !mount) return false;
	    var backend = mount.getAttribute("data-gosx-scene3d-renderer") || "starting";
	    var fallback = mount.getAttribute("data-gosx-scene3d-renderer-fallback") || "";
	    output.textContent = title(backend) + (fallback ? " · fallback" : "");
	    output.setAttribute("data-backend", backend);
	    return backend !== "starting";
	  }
	  function boot() {
	    if (sync()) return;
	    var attempts = 0;
	    var timer = setInterval(function() {
	      attempts++;
	      if (sync() || attempts >= 80) clearInterval(timer);
	    }, 100);
	  }
	  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", boot);
	  else boot();
	})();
	`}
	</script>
}
