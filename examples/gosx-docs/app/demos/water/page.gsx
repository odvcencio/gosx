package docs

func Page() Node {
  return <main
	class="water-demo"
	data-gosx-scene3d-control-scope="true"
	data-gosx-scene3d-panel-scope="true"
	aria-label="GoSX water simulation demo"
  >
	<Scene3D
		id="water-demo-scene"
		class="water-demo__scene"
		width={1280}
		height={760}
		responsive={true}
		fillHeight={true}
		controls="orbit"
		controlTargetX={0}
		controlTargetY={-0.5}
		controlTargetZ={0}
		controlRotateMode="pixel-degrees"
		controlMinDistance={2}
		controlMaxDistance={10}
		controlPitchLimit={1.5707788735}
		preferWebGPU={true}
		maxDevicePixelRatio={1.6}
		adaptiveQuality={true}
		qualityTier="balanced"
		canvasAlpha={false}
	>
		<Camera
			x={1.2695827068526726}
			y={1.1904730469627978}
			z={3.395653196065958}
			fov={45}
			near={0.01}
			far={100}
		 />
		<Environment
			ambientColor="#d8edf2"
			ambientIntensity={0.2}
			skyColor="#89b7c5"
			skyIntensity={0.36}
			groundColor="#211813"
			groundIntensity={0.24}
			fogColor="#071217"
			fogDensity={0.016}
		 />
		<WaterSystem
			id="water-main"
			interactionProfile="water-object-drop-orbit"
			interactionTarget="water-main"
			interactionObject="Sphere"
			resolution={192}
			poolShape="Box"
			poolWidth={1.0}
			poolHeight={1.0}
			poolLength={1.0}
			cornerRadius={0}
			waveSpeed={1.0}
			damping={0.995}
			normalScale={1.0}
			seedDrops={20}
			dropRadius={0.03}
			dropStrength={0.01}
			tileTexture="/water/tiles.jpg"
			cubeMap="/water/"
			shallowColor="#7ad1eb"
			deepColor="#082e57"
			causticsResolution={512}
			objectTextureResolutionMode="viewport"
			objectTexturePixelBudget={786432}
			objectShadowResolution={512}
			caustics={true}
			reflection={true}
			refraction={true}
			followCamera={false}
			lightDirectionX={2}
			lightDirectionY={2}
			lightDirectionZ={-1}
			activeObject="Sphere"
			objectKind="sphere"
			objectX={-0.4}
			objectY={-0.75}
			objectZ={0.2}
			objectRadius={0.25}
			objectDriftX={0}
			objectDriftY={0}
			objectDriftZ={0}
			objectBobAmplitude={0}
			objectBobSpeed={0}
			objectDisplacementScale={1.0}
			computeBackend="elio"
			materialBackend="selena"
			seedSelenaWGSL={data.waterSeedSelenaWGSL}
			dropSelenaWGSL={data.waterDropSelenaWGSL}
			displacementSelenaWGSL={data.waterDisplacementSelenaWGSL}
			simulationSelenaWGSL={data.waterSimulationSelenaWGSL}
			normalSelenaWGSL={data.waterNormalSelenaWGSL}
			poolSelenaWGSL={data.waterPoolSelenaWGSL}
			surfaceSelenaWGSL={data.waterSurfaceSelenaWGSL}
			surfaceBelowSelenaWGSL={data.waterSurfaceBelowSelenaWGSL}
			causticsSelenaWGSL={data.waterCausticsSelenaWGSL}
			objectShadowSelenaWGSL={data.waterObjectShadowSelenaWGSL}
			compoundShadowSelenaWGSL={data.waterCompoundShadowSelenaWGSL}
			objectMeshShadowSelenaWGSL={data.waterObjectMeshShadowSelenaWGSL}
			seedVertexGLES={data.waterSeedVertexGLES}
			seedFragmentGLES={data.waterSeedFragmentGLES}
			dropVertexGLES={data.waterDropVertexGLES}
			dropFragmentGLES={data.waterDropFragmentGLES}
			displacementVertexGLES={data.waterDisplacementVertexGLES}
			displacementFragmentGLES={data.waterDisplacementFragmentGLES}
			simulationVertexGLES={data.waterSimulationVertexGLES}
			simulationFragmentGLES={data.waterSimulationFragmentGLES}
			normalVertexGLES={data.waterNormalVertexGLES}
			normalFragmentGLES={data.waterNormalFragmentGLES}
			causticsVertexGLES={data.waterCausticsVertexGLES}
			causticsFragmentGLES={data.waterCausticsFragmentGLES}
			poolVertexGLES={data.waterPoolVertexGLES}
			poolFragmentGLES={data.waterPoolFragmentGLES}
			surfaceVertexGLES={data.waterSurfaceVertexGLES}
			surfaceFragmentGLES={data.waterSurfaceFragmentGLES}
			surfaceBelowVertexGLES={data.waterSurfaceBelowVertexGLES}
			surfaceBelowFragmentGLES={data.waterSurfaceBelowFragmentGLES}
			objectShadowVertexGLES={data.waterObjectShadowVertexGLES}
			objectShadowFragmentGLES={data.waterObjectShadowFragmentGLES}
			compoundShadowVertexGLES={data.waterCompoundShadowVertexGLES}
			compoundShadowFragmentGLES={data.waterCompoundShadowFragmentGLES}
			objectMeshShadowVertexGLES={data.waterObjectMeshShadowVertexGLES}
			objectMeshShadowFragmentGLES={data.waterObjectMeshShadowFragmentGLES}
			objectMaterialVertexGLES={data.waterObjectPassVertexGLES}
			objectMaterialFragmentGLES={data.waterObjectPassFragmentGLES}
			duckMaterialVertexGLES={data.waterDuckPassVertexGLES}
			duckMaterialFragmentGLES={data.waterDuckPassFragmentGLES}
			shaderDescriptors={data.waterShaderDescriptors}
		 />
		<Material
			name="brass-glow"
			kind="standard"
			color="#d9974d"
			roughness={0.34}
			metalness={0.28}
			emissive="#513318"
			emissiveIntensity={0.28}
		 />
		<Material
			name="water-object-material"
			kind="custom"
			shaderBackend="selena"
			customVertexWGSL={data.waterObjectPassSelenaWGSL}
			customFragmentWGSL={data.waterObjectPassSelenaWGSL}
			shaderLayout={data.waterObjectMaterialSelenaLayout}
			customUniforms={data.waterObjectMaterialSelenaUniforms}
		 />
		<Material
			name="water-duck-material"
			kind="custom"
			shaderBackend="selena"
			customVertexWGSL={data.waterDuckPassSelenaWGSL}
			customFragmentWGSL={data.waterDuckPassSelenaWGSL}
			shaderLayout={data.waterDuckMaterialSelenaLayout}
			customUniforms={data.waterDuckMaterialSelenaUniforms}
		 />
		<Mesh
			id="float-sphere"
			kind="sphere"
			radius={0.25}
			x={-0.4}
			y={-0.75}
			z={0.2}
			material="water-object-material"
			roughness={0.32}
			metalness={0.08}
			wireframe={false}
			castShadow={true}
			spinX={0}
			spinY={0}
			driftX={0}
			driftY={0}
			driftZ={0}
			bobAmplitude={0}
			bobSpeed={0}
			outlineColor="#ffe5c8"
			outlineWidth={0.022}
		 />
		<Mesh
			id="float-cube"
			kind="box"
			width={0.5}
			height={0.5}
			depth={0.5}
			x={-0.4}
			y={10}
			visible={false}
			z={0.2}
			rotationX={0}
			rotationY={0}
			material="water-object-material"
			roughness={0.42}
			metalness={0.1}
			wireframe={false}
			castShadow={true}
			spinX={0.2}
			spinY={0.28}
			driftX={0}
			driftY={0}
			driftZ={0}
			bobAmplitude={0}
			bobSpeed={0}
		 />
		<Mesh
			id="float-torus"
			kind="torusKnot"
			radius={0.17}
			tube={0.045}
			tubularSegments={64}
			radialSegments={8}
			x={-0.4}
			y={10}
			visible={false}
			z={0.2}
			rotationX={1.5707963267948966}
			material="water-object-material"
			wireframe={false}
			castShadow={true}
			spinX={0}
			spinY={0}
			driftX={0}
			driftY={0}
			driftZ={0}
			bobAmplitude={0}
			bobSpeed={0}
		 />
		</Scene3D>
		<section class="water-demo__proof" aria-label="Live GoSX runtime proof">
			<p class="water-demo__proof-line">
				<strong>GoSX Scene3D</strong>
				<span aria-hidden="true">·</span>
				<output id="water-proof-backend" aria-live="polite">starting</output>
				<span aria-hidden="true">·</span>
				<output id="water-proof-tier">balanced</output>
				<span aria-hidden="true">·</span>
				<span>60 Hz sim</span>
				<span aria-hidden="true">·</span>
				<span>DPR <output id="water-proof-dpr">1.25</output></span>
			</p>
			<div class="water-demo__proof-actions">
				<output id="water-proof-model" class="water-demo__model-proof" data-state="deferred">Duck glTF · deferred</output>
				<button id="water-story-open" class="water-demo__story-open" type="button">How this is GoSX</button>
			</div>
		</section>
		<dialog id="water-story" class="water-demo__story" aria-labelledby="water-story-title">
			<div class="water-demo__story-head">
				<div>
					<p class="water-demo__story-kicker">Framework proof</p>
					<h2 id="water-story-title">A native GoSX water system</h2>
				</div>
				<button id="water-story-close" class="water-demo__story-close" type="button" aria-label="Close GoSX story">Close</button>
			</div>
			<pre class="water-demo__story-source" tabindex="0"><code>{`<Scene3D preferWebGPU={true} adaptiveQuality={true}>
  <WaterSystem
    computeBackend="elio"
    materialBackend="selena"
    caustics={true}
    reflection={true}
    refraction={true}
  />
</Scene3D>`}</code></pre>
			<ul class="water-demo__story-facts">
				<li>Typed .gsx lowers to GoSX SceneIR.</li>
				<li>Selena emits WGSL and GLES from one shader source.</li>
				<li>GoSX selects its WebGPU or WebGL2 renderer at runtime.</li>
				<li>Adaptive quality and offscreen suspension protect frame time.</li>
			</ul>
			<a class="water-demo__source-link" href="https://github.com/odvcencio/gosx/blob/main/examples/gosx-docs/app/demos/water/page.gsx" target="_blank" rel="noopener noreferrer">View GoSX source</a>
		</dialog>
	<aside
		id="water-demo-help"
		class="water-demo__help"
		data-gosx-scene3d-help-panel="true"
		aria-label="Water demo reference"
	>
		<h1>GoSX Water</h1>
		<p>
			Original author:
			<a href="http://madebyevan.com/" target="_blank" rel="noopener noreferrer">Evan Wallace</a>
		</p>
		<p>
			Ported to Three.js by
			<a href="https://github.com/jeantimex" target="_blank" rel="noopener noreferrer">jeantimex</a>
		</p>
		<p>
			<a href="https://github.com/jeantimex/threejs-water" target="_blank" rel="noopener noreferrer">jeantimex/threejs-water</a>
		</p>
		<p>
			Ported to GoSX by
			<a href="https://github.com/odvcencio" target="_blank" rel="noopener noreferrer">odvcencio</a>
			and
			<a href="https://github.com/m31-labs" target="_blank" rel="noopener noreferrer">M31 Labs</a>
		</p>
		<h2>Interactions</h2>
		<ul>
			<li>Draw on the water to make ripples</li>
			<li>
				Drag the background to rotate the camera
			</li>
			<li>Scroll or pinch to zoom</li>
			<li>Press SPACEBAR to pause and unpause</li>
			<li>
				Drag the selected object to move it around
			</li>
			<li>
				Press the L key to set the light direction
			</li>
			<li>Press the G key to toggle gravity</li>
		</ul>
		<h2>Features</h2>
		<ul>
				<li>Ray-marched reflections and refractions</li>
				<li>Analytic object and pool shading</li>
			<li>Heightfield water simulation</li>
			<li>Soft shadows</li>
			<li>Real-time caustics</li>
			<li>
					Box and rounded-box pool geometry
			</li>
			<li>
				Procedural geometry and glTF model support
			</li>
		</ul>
	</aside>
	<button
		class="water-demo__help-toggle"
		type="button"
		aria-controls="water-demo-help"
		aria-expanded="false"
		data-gosx-scene3d-panel-toggle="water-demo-help"
	>menu</button>
	<form
		class="water-demo__controls"
		data-gosx-scene3d-controls="true"
		data-gosx-scene3d-control-form="fluid-object"
		data-gosx-scene3d-control-subject="water-main"
		data-gosx-scene3d-control-target="water-demo-scene"
		data-gosx-scene3d-control-data={data.waterControlData}
		data-gosx-scene3d-control-open="false"
		aria-label="Water settings"
	>
		<div class="water-demo__controls-head">
			<button
				class="water-demo__controls-toggle"
				type="button"
				aria-expanded="false"
				data-gosx-scene3d-control-toggle="true"
			>Settings</button>
			<output name="status" data-gosx-scene3d-control-status="true">Sphere</output>
		</div>
		<div class="water-demo__controls-body" data-gosx-scene3d-control-body="true">
			<fieldset class="water-demo__control-group" data-gosx-scene3d-control-group="Scene">
				<legend>Scene</legend>
				<label class="water-demo__toggle">
					<input type="checkbox" name="paused" />
					<span>Paused</span>
				</label>
			</fieldset>
			<fieldset class="water-demo__control-group" data-gosx-scene3d-control-group="Object">
				<legend>Object</legend>
				<label>
					<span>Object</span>
					<select name="object">
						<option value="None">None</option>
						<option value="Sphere" selected={true}>Sphere</option>
						<option value="Cube">Cube</option>
						<option value="TorusKnot">TorusKnot</option>
						<option value="Rubber Duck">Rubber Duck</option>
					</select>
				</label>
				<div class="water-demo__control-grid">
					<label class="water-demo__toggle">
						<input type="checkbox" name="gravity" />
						<span>Gravity</span>
					</label>
					<label class="water-demo__toggle">
						<input type="checkbox" name="densityEnabled" />
						<span>Density</span>
					</label>
				</div>
				<label data-gosx-scene3d-density-control="true">
					<span>Density</span>
					<input type="range" name="density" min="0.2" max="2" step="0.01" value="0.9" />
				</label>
			</fieldset>
			<fieldset class="water-demo__control-group" data-gosx-scene3d-control-group="Pool">
				<legend>Pool</legend>
				<label>
					<span>Pool Shape</span>
					<select name="poolShape">
						<option value="Box" selected={true}>Box</option>
						<option value="Rounded Box">Rounded Box</option>
					</select>
				</label>
				<label data-gosx-scene3d-rounded-control="true">
					<span>Corner Radius</span>
					<input type="range" name="cornerRadius" min="0" max="1" step="0.01" value="0.1" />
				</label>
				<label data-gosx-scene3d-pool-boundary-control="true">
					<span>Pool Width</span>
					<input type="range" name="poolWidth" min="0.5" max="3" step="0.05" value="1" />
				</label>
				<label data-gosx-scene3d-pool-boundary-control="true">
					<span>Pool Depth</span>
					<input type="range" name="poolHeight" min="0.3" max="2" step="0.05" value="1" />
				</label>
				<label data-gosx-scene3d-pool-boundary-control="true">
					<span>Pool Length</span>
					<input type="range" name="poolLength" min="0.5" max="3" step="0.05" value="1" />
				</label>
			</fieldset>
			<fieldset class="water-demo__control-group" data-gosx-scene3d-control-group="Lights">
				<legend>Lights</legend>
				<label class="water-demo__toggle">
					<input type="checkbox" name="followCamera" />
					<span>Follow Camera</span>
				</label>
			</fieldset>
		</div>
		</form>
		<script>
			{`
(function () {
  if (typeof document === "undefined") return;
  var root = document.querySelector(".water-demo");
  var mount = document.getElementById("water-demo-scene");
  var backend = document.getElementById("water-proof-backend");
  var tier = document.getElementById("water-proof-tier");
  var dpr = document.getElementById("water-proof-dpr");
  var model = document.getElementById("water-proof-model");
  var dialog = document.getElementById("water-story");
  var open = document.getElementById("water-story-open");
  var close = document.getElementById("water-story-close");
  if (!root || !mount) return;

  function attr(name, fallback) {
    var value = mount.getAttribute(name);
    return value ? value : fallback;
  }
  function title(value) {
    value = String(value || "");
    return value ? value.charAt(0).toUpperCase() + value.slice(1) : value;
  }
  function updateProof() {
    if (backend) backend.textContent = title(attr("data-gosx-scene3d-renderer", "starting"));
    if (tier) tier.textContent = title(attr("data-gosx-scene3d-quality-active", "balanced"));
    if (dpr) dpr.textContent = String(Number(attr("data-gosx-scene3d-quality-dpr-cap", "1.25")).toFixed(2)).replace(/0+$/, "").replace(/\.$/, "");
  }
  function updateModel(detail) {
    if (!model || !detail || !/\/water\/models\/duck\/Duck\.gltf(?:\?|$)/i.test(detail.asset || "")) return;
    var status = detail.status || "deferred";
    var labels = {
      loading: "Duck glTF · loading",
      loaded: "Duck glTF · loaded on demand",
      cached: "Duck glTF · cached",
      error: "Duck glTF · unavailable"
    };
    model.dataset.state = status;
    model.textContent = labels[status] || "Duck glTF · deferred";
  }
  mount.addEventListener("gosx:scene3d:model-status", function (event) {
    updateModel(event.detail || {});
  });
  updateProof();
  var proofTimer = window.setInterval(updateProof, 250);
  window.addEventListener("pagehide", function () { window.clearInterval(proofTimer); }, { once: true });

  if (dialog && open && close && typeof dialog.showModal === "function") {
    open.addEventListener("click", function () { dialog.showModal(); });
    close.addEventListener("click", function () { dialog.close(); });
    dialog.addEventListener("click", function (event) {
      if (event.target === dialog) dialog.close();
    });
  }
})();
			`}
		</script>
	</main>
}
