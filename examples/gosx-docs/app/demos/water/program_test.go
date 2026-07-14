package docs

import (
	"encoding/json"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/selena/bindings"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWaterDemoPreloadHead(t *testing.T) {
	ctx := &route.RouteContext{}
	addWaterDemoPreloadHead(ctx)
	head := gosx.RenderHTML(ctx.Head())
	for _, href := range []string{
		"/water/tiles.jpg",
		"/water/xpos.jpg",
		"/water/xneg.jpg",
		"/water/ypos.jpg",
		"/water/zpos.jpg",
		"/water/zneg.jpg",
	} {
		want := `rel="preload" as="image" href="` + href + `" crossorigin="anonymous"`
		if !strings.Contains(head, want) {
			t.Fatalf("preload head missing %s in %s", want, head)
		}
	}
}

func TestWaterDemoDataCompiles(t *testing.T) {
	data, err := WaterDemoData()
	if err != nil {
		t.Fatalf("WaterDemoData returned error: %v", err)
	}
	// waterSelenaWGSLKeys is one "<dataPrefix>SelenaWGSL" key per compute
	// kernel/render pass Selena compiles (selena_glsl.go's
	// waterSelenaComputeWGSLData/waterSelenaRenderWGSLData) -- the sole
	// primary WGSL source for every WebGPU water pass now that the
	// hand-written Elio/Selena shader trees (shaders/jeantimex-water.elio/,
	// shaders/jeantimex-water.sel/) have been retired. page.gsx wires each of
	// these into <WaterSystem>'s *SelenaWGSL props (or, for
	// waterObjectPassSelenaWGSL/waterDuckPassSelenaWGSL, the two water
	// <Material> blocks' customVertexWGSL/customFragmentWGSL).
	waterSelenaWGSLKeys := []string{
		"waterSeedSelenaWGSL", "waterDropSelenaWGSL", "waterDisplacementSelenaWGSL", "waterSimulationSelenaWGSL", "waterNormalSelenaWGSL",
		"waterPoolSelenaWGSL", "waterSurfaceSelenaWGSL", "waterSurfaceBelowSelenaWGSL", "waterCausticsSelenaWGSL",
		"waterObjectShadowSelenaWGSL", "waterCompoundShadowSelenaWGSL", "waterObjectMeshShadowSelenaWGSL",
		"waterObjectPassSelenaWGSL", "waterDuckPassSelenaWGSL",
	}
	requiredKeys := append([]string{
		"waterControlData",
		"waterObjectMaterialSelenaUniforms", "waterDuckMaterialSelenaUniforms",
		"waterObjectMaterialSelenaLayout", "waterDuckMaterialSelenaLayout",
	}, waterSelenaWGSLKeys...)
	for _, key := range requiredKeys {
		if data[key] == nil {
			t.Fatalf("WaterDemoData missing %q", key)
		}
	}
	controlJSON, ok := data["waterControlData"].(string)
	if !ok || controlJSON == "" {
		t.Fatalf("waterControlData = %T %#v, want non-empty string", data["waterControlData"], data["waterControlData"])
	}
	for _, key := range waterSelenaWGSLKeys {
		wgsl, ok := data[key].(string)
		if !ok || strings.TrimSpace(wgsl) == "" {
			t.Fatalf("WaterDemoData[%s] = %T len=%d, want non-empty Selena-compiled WGSL", key, data[key], len(wgsl))
		}
	}
	objectSelenaUniforms, ok := data["waterObjectMaterialSelenaUniforms"].(map[string]any)
	if !ok {
		t.Fatalf("waterObjectMaterialSelenaUniforms = %T, want map[string]any", data["waterObjectMaterialSelenaUniforms"])
	}
	if objectSelenaUniforms["water"] != "gosx:water:water-main:state" {
		t.Fatalf("waterObjectMaterialSelenaUniforms missing live water resource ref: %#v", objectSelenaUniforms)
	}
	duckSelenaUniforms, ok := data["waterDuckMaterialSelenaUniforms"].(map[string]any)
	if !ok || duckSelenaUniforms["modelTexture"] != "/water/models/duck/DuckCM.png" || duckSelenaUniforms["water"] != "gosx:water:water-main:state" {
		t.Fatalf("waterDuckMaterialSelenaUniforms = %#v, want duck texture and live water refs", data["waterDuckMaterialSelenaUniforms"])
	}
	// The retired hand-written Elio/Selena shader trees' compiled bodies,
	// file-path provenance, and bespoke layout/uniform maps must not
	// resurface in WaterDemoData -- Selena (shaders/jeantimex-water.selena/)
	// is the sole primary WGSL source for every water pass now.
	for _, stale := range []string{
		"waterComputeSource", "waterMaterialSource", "waterComputeSourceFiles", "waterMaterialSourceFiles",
		"waterObjectMaterialSource", "waterObjectMaterialSourceFiles", "waterDuckMaterialSource", "waterDuckMaterialSourceFiles",
		"waterSeedWGSL", "waterDropWGSL", "waterDisplacementWGSL", "waterSimulationWGSL", "waterNormalWGSL",
		"waterCausticsWGSL", "waterPoolVertexWGSL", "waterPoolFragmentWGSL",
		"waterSurfaceVertexWGSL", "waterSurfaceFragmentWGSL", "waterSurfaceBelowFragmentWGSL",
		"waterObjectShadowWGSL", "waterObjectMeshShadowVertexWGSL", "waterObjectMeshShadowFragmentWGSL",
		"waterObjectMaterialWGSL", "waterObjectMaterialLayout", "waterObjectMaterialUniforms",
		"waterDuckMaterialWGSL", "waterDuckMaterialLayout", "waterDuckMaterialUniforms",
	} {
		if _, ok := data[stale]; ok {
			t.Fatalf("WaterDemoData still exposes retired hand-written key %q", stale)
		}
	}
	var controlProfile map[string]any
	if err := json.Unmarshal([]byte(controlJSON), &controlProfile); err != nil {
		t.Fatalf("waterControlData is not valid JSON: %v", err)
	}
	if waterDemoHiddenY != 10 {
		t.Fatalf("waterDemoHiddenY = %v, want upstream inactive object Y=10", waterDemoHiddenY)
	}
	if hiddenY, ok := controlProfile["hiddenY"].(float64); !ok || hiddenY != waterDemoHiddenY {
		t.Fatalf("waterControlData hiddenY = %#v, want %v", controlProfile["hiddenY"], waterDemoHiddenY)
	}
	if inactiveY, ok := controlProfile["inactiveY"].(float64); !ok || inactiveY != waterDemoHiddenY {
		t.Fatalf("waterControlData inactiveY = %#v, want %v", controlProfile["inactiveY"], waterDemoHiddenY)
	}
	physics, ok := controlProfile["physics"].(map[string]any)
	if !ok {
		t.Fatalf("waterControlData physics = %T, want map[string]any", controlProfile["physics"])
	}
	if physics["gravityY"] != float64(-4) || physics["bounce"] != 0.7 || physics["defaultBuoyancyScale"] != 1.1 {
		t.Fatalf("waterControlData physics = %#v", physics)
	}
	interaction, ok := controlProfile["interaction"].(map[string]any)
	if !ok {
		t.Fatalf("waterControlData interaction = %T, want map[string]any", controlProfile["interaction"])
	}
	if interaction["profile"] != "water-object-drop-orbit" || interaction["pointerDrops"] != true || interaction["keyboard"] != true || interaction["dropRadius"] != 0.03 || interaction["dropStrength"] != 0.01 {
		t.Fatalf("waterControlData interaction = %#v", interaction)
	}
	objects, ok := controlProfile["objects"].(map[string]any)
	if !ok {
		t.Fatalf("waterControlData objects = %T, want map[string]any", controlProfile["objects"])
	}
	for _, name := range []string{"Sphere", "Cube", "TorusKnot", "Rubber Duck"} {
		object, ok := objects[name].(map[string]any)
		if !ok {
			t.Fatalf("waterControlData missing object profile %q", name)
		}
		for _, field := range []string{"buoyancyRadius", "floorClearance", "xLimitRadius", "zLimitRadius", "meshYOffset"} {
			if _, ok := object[field].(float64); !ok {
				t.Fatalf("%s missing physics field %q: %#v", name, field, object[field])
			}
		}
		mesh, ok := object["mesh"].(map[string]any)
		if !ok {
			t.Fatalf("%s missing dynamic mesh patch: %#v", name, object["mesh"])
		}
		if mesh["visible"] != true {
			t.Fatalf("%s mesh visible = %#v, want true active patch", name, mesh["visible"])
		}
	}
	for name, want := range map[string]string{
		"Sphere":      "sphere",
		"Cube":        "box",
		"TorusKnot":   "mesh",
		"Rubber Duck": "sphere",
	} {
		object := objects[name].(map[string]any)
		if got := object["objectHitTest"]; got != want {
			t.Fatalf("%s objectHitTest = %#v, want %q", name, got, want)
		}
	}
	duck := objects["Rubber Duck"].(map[string]any)
	if duck["src"] != "/water/models/duck/Duck.gltf" {
		t.Fatalf("Duck src = %#v, want route-authored Duck glTF URL", duck["src"])
	}
	if duck["objectSubtype"] != "duck" {
		t.Fatalf("Duck objectSubtype = %#v, want duck", duck["objectSubtype"])
	}
	duckMesh, ok := duck["mesh"].(map[string]any)
	if !ok || duckMesh["material"] != "water-duck-material" || duckMesh["castShadow"] != true || duckMesh["receiveShadow"] != true {
		t.Fatalf("Duck mesh patch = %#v, want material and shadow flags for dynamic model patches", duck["mesh"])
	}
	if duckMesh["rotationY"] != float64(0) || duckMesh["scaleX"] != float64(1) || duckMesh["scaleY"] != float64(1) || duckMesh["scaleZ"] != float64(1) {
		t.Fatalf("Duck mesh transform = %#v, want upstream-neutral transform before Scene3D model fit", duckMesh)
	}
	if duckMesh["bounds"] != float64(0.5) || duckMesh["fit"] != "contain" || duckMesh["fitAlign"] != "center-min-y" {
		t.Fatalf("Duck mesh fit metadata = %#v, want upstream duck normalization metadata", duckMesh)
	}
	duckSpheres, ok := duck["objectDisplacementSpheres"].([]any)
	if !ok || len(duckSpheres) != 3 {
		t.Fatalf("Duck objectDisplacementSpheres = %#v, want upstream 3-sphere volume approximation", duck["objectDisplacementSpheres"])
	}
	for index, want := range []map[string]float64{
		{"offsetX": 0, "offsetY": 0, "offsetZ": 0, "radius": 0.15},
		{"offsetX": 0, "offsetY": 0.1, "offsetZ": 0.1, "radius": 0.08},
		{"offsetX": 0, "offsetY": -0.08, "offsetZ": -0.05, "radius": 0.1},
	} {
		sphere, ok := duckSpheres[index].(map[string]any)
		if !ok {
			t.Fatalf("Duck displacement sphere %d = %T, want map[string]any", index, duckSpheres[index])
		}
		for field, wantValue := range want {
			if got, ok := sphere[field].(float64); !ok || got != wantValue {
				t.Fatalf("Duck displacement sphere %d field %s = %#v, want %v", index, field, sphere[field], wantValue)
			}
		}
	}
	torus := objects["TorusKnot"].(map[string]any)
	if torus["objectSubtype"] != "torusKnot" {
		t.Fatalf("TorusKnot objectSubtype = %#v, want torusKnot", torus["objectSubtype"])
	}
	torusMesh := torus["mesh"].(map[string]any)
	if got, ok := torusMesh["rotationX"].(float64); !ok || math.Abs(got-math.Pi/2) > 0.0000001 {
		t.Fatalf("TorusKnot mesh rotationX = %#v, want upstream flat Math.PI/2 orientation", torusMesh["rotationX"])
	}
	for _, name := range []string{"TorusKnot", "Rubber Duck"} {
		object := objects[name].(map[string]any)
		spheres, ok := object["objectDisplacementSpheres"].([]any)
		if !ok || len(spheres) == 0 {
			t.Fatalf("%s objectDisplacementSpheres = %#v, want non-empty list", name, object["objectDisplacementSpheres"])
		}
		if object["objectKind"] != "compound" {
			t.Fatalf("%s objectKind = %#v, want compound", name, object["objectKind"])
		}
	}
	if _, ok := data["waterMaterial"]; ok {
		t.Fatal("WaterDemoData still exposes route-side waterMaterial instead of using WaterSystem")
	}
	if _, ok := data["causticPrisms"]; ok {
		t.Fatal("WaterDemoData still exposes route-side causticPrisms instead of using WaterSystem caustics")
	}
	if _, ok := data["roundedPoolFloor"]; ok {
		t.Fatal("WaterDemoData still exposes route-side roundedPoolFloor geometry")
	}
	if _, ok := data["poolRimPoints"]; ok {
		t.Fatal("WaterDemoData still exposes route-side pool rim geometry instead of using WaterSystem pool pass")
	}
}

func TestWaterDemoControlsContract(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	pageBytes, err := os.ReadFile(filepath.Join(dir, "page.gsx"))
	if err != nil {
		t.Fatalf("read page.gsx: %v", err)
	}
	cssBytes, err := os.ReadFile(filepath.Join(dir, "page.css"))
	if err != nil {
		t.Fatalf("read page.css: %v", err)
	}
	runtimeBytes, err := os.ReadFile(filepath.Join(dir, "../../../../../client/js/bootstrap-src/19b-scene-control-forms.js"))
	if err != nil {
		t.Fatalf("read managed water controls runtime: %v", err)
	}
	webgpuBytes, err := os.ReadFile(filepath.Join(dir, "../../../../../client/js/bootstrap-src/16a-scene-webgpu.js"))
	if err != nil {
		t.Fatalf("read Scene3D WebGPU runtime: %v", err)
	}
	programBytes, err := os.ReadFile(filepath.Join(dir, "program.go"))
	if err != nil {
		t.Fatalf("read program.go: %v", err)
	}
	page := string(pageBytes)
	css := string(cssBytes)
	runtimeSource := string(runtimeBytes)
	webgpuSource := string(webgpuBytes)
	program := string(programBytes)
	if got := strings.Count(page, `y={10}`); got != 2 {
		t.Fatalf("water page hidden object y count = %d, want 2 eagerly authored inactive meshes", got)
	}
	if got := strings.Count(page, `visible={false}`); got != 2 {
		t.Fatalf("water page hidden object visibility count = %d, want 2 eagerly authored inactive meshes", got)
	}

	if strings.Contains(page, `data-water-`) {
		t.Fatalf("page.gsx still declares route-specific data-water attributes")
	}
	if strings.Contains(page, `<PostFX.`) {
		t.Fatalf("page.gsx still declares route-level post-processing for the upstream water demo")
	}

	for _, want := range []string{
		`data-gosx-scene3d-controls="true"`,
		`data-gosx-scene3d-control-form="fluid-object"`,
		`data-gosx-scene3d-control-subject="water-main"`,
		`data-gosx-scene3d-control-target="water-demo-scene"`,
		`data-gosx-scene3d-control-data={data.waterControlData}`,
		`data-gosx-scene3d-control-scope="true"`,
		`data-gosx-scene3d-panel-scope="true"`,
		`data-gosx-scene3d-control-open="false"`,
		`data-gosx-scene3d-control-toggle="true"`,
		`data-gosx-scene3d-control-body="true"`,
		`data-gosx-scene3d-control-group="Scene"`,
		`data-gosx-scene3d-control-group="Object"`,
		`data-gosx-scene3d-control-group="Pool"`,
		`data-gosx-scene3d-control-group="Lights"`,
		`data-gosx-scene3d-panel-toggle="water-demo-help"`,
		`data-gosx-scene3d-help-panel="true"`,
		`GoSX Water`,
		`jeantimex/threejs-water`,
		`Ported to GoSX by`,
		`Scroll or pinch to zoom`,
		`Press SPACEBAR to pause and unpause`,
		`controlTargetY={-0.5}`,
		`controlRotateMode="pixel-degrees"`,
		`controlRotateDirection="grab"`,
		`controlMinDistance={2}`,
		`controlMaxDistance={10}`,
		`controlPitchLimit={1.5707788735}`,
		`x={1.2695827068526726}`,
		`y={1.1904730469627978}`,
		`z={3.395653196065958}`,
		`interactionProfile="water-object-drop-orbit"`,
		`interactionTarget="water-main"`,
		`interactionObject="Sphere"`,
		`poolWidth={1.0}`,
		`poolHeight={1.0}`,
		`poolLength={1.0}`,
		`objectX={-0.4}`,
		`objectY={-0.75}`,
		`objectZ={0.2}`,
		`objectRadius={0.25}`,
		`name="object"`,
		`value="TorusKnot"`,
		`value="Rubber Duck"`,
		`name="gravity"`,
		`name="densityEnabled"`,
		`name="poolShape"`,
		`name="cornerRadius" min="0" max="1" step="0.01" value="0.1"`,
		`name="poolWidth" min="0.5" max="3" step="0.05" value="1"`,
		`name="poolHeight" min="0.3" max="2" step="0.05" value="1"`,
		`name="poolLength" min="0.5" max="3" step="0.05" value="1"`,
		`radius={0.25}`,
		`width={0.5}`,
		`tubularSegments={64}`,
		`radialSegments={8}`,
		`rotationX={1.5707963267948966}`,
		`data-gosx-scene3d-rounded-control="true"`,
		`data-gosx-scene3d-pool-boundary-control="true"`,
		`cubeMap="/water/"`,
		`shallowColor="#7ad1eb"`,
		`deepColor="#082e57"`,
		`aboveWaterColorR={0.25}`,
		`aboveWaterColorG={1.0}`,
		`aboveWaterColorB={1.25}`,
		`lightDirectionX={2}`,
		`lightDirectionY={2}`,
		`lightDirectionZ={-1}`,
		`waveSpeed={1.0}`,
		`damping={0.995}`,
		`adaptiveQuality={false}`,
		`qualityTier="full"`,
		`resolution={256}`,
		`surfaceResolution={201}`,
		`causticsResolution={1024}`,
		`objectTextureResolutionMode="viewport"`,
		`objectShadowResolution={1024}`,
		// Selena-compiled combined-WGSL slots: the sole primary WGSL source
		// for every water compute kernel/render pass now that the
		// hand-written Elio/Selena *WGSL props have been retired.
		`seedSelenaWGSL={data.waterSeedSelenaWGSL}`,
		`dropSelenaWGSL={data.waterDropSelenaWGSL}`,
		`displacementSelenaWGSL={data.waterDisplacementSelenaWGSL}`,
		`simulationSelenaWGSL={data.waterSimulationSelenaWGSL}`,
		`normalSelenaWGSL={data.waterNormalSelenaWGSL}`,
		`poolSelenaWGSL={data.waterPoolSelenaWGSL}`,
		`surfaceSelenaWGSL={data.waterSurfaceSelenaWGSL}`,
		`surfaceBelowSelenaWGSL={data.waterSurfaceBelowSelenaWGSL}`,
		`causticsSelenaWGSL={data.waterCausticsSelenaWGSL}`,
		`objectShadowSelenaWGSL={data.waterObjectShadowSelenaWGSL}`,
		`compoundShadowSelenaWGSL={data.waterCompoundShadowSelenaWGSL}`,
		`objectMeshShadowSelenaWGSL={data.waterObjectMeshShadowSelenaWGSL}`,
		`name="water-object-material"`,
		`kind="custom"`,
		`shaderBackend="selena"`,
		`customVertexWGSL={data.waterObjectPassSelenaWGSL}`,
		`customFragmentWGSL={data.waterObjectPassSelenaWGSL}`,
		`shaderLayout={data.waterObjectMaterialSelenaLayout}`,
		`customUniforms={data.waterObjectMaterialSelenaUniforms}`,
		`name="water-duck-material"`,
		`customVertexWGSL={data.waterDuckPassSelenaWGSL}`,
		`customFragmentWGSL={data.waterDuckPassSelenaWGSL}`,
		`shaderLayout={data.waterDuckMaterialSelenaLayout}`,
		`customUniforms={data.waterDuckMaterialSelenaUniforms}`,
		`material="water-object-material"`,
		`castShadow={true}`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("page.gsx missing %q", want)
		}
	}
	if strings.Contains(page, `<Model`) || strings.Contains(page, `VertexGLSL={data.`) || strings.Contains(page, `FragmentGLSL={data.`) {
		t.Fatal("water page eagerly embeds the Duck model or unused desktop GLSL payload")
	}
	for _, want := range []string{`/water/models/duck/Duck.gltf`, `"material": "water-duck-material"`, `"receiveShadow": true`} {
		if !strings.Contains(program, want) {
			t.Fatalf("program.go lazy Duck descriptor missing %q", want)
		}
	}
	objectOptionOrder := []string{`value="None"`, `value="Sphere"`, `value="Cube"`, `value="TorusKnot"`, `value="Rubber Duck"`}
	lastObjectOptionIndex := -1
	for _, option := range objectOptionOrder {
		index := strings.Index(page, option)
		if index < 0 {
			t.Fatalf("page.gsx missing object option %q", option)
		}
		if index <= lastObjectOptionIndex {
			t.Fatalf("page.gsx object option %q appears out of upstream order", option)
		}
		lastObjectOptionIndex = index
	}
	for _, unwanted := range []string{
		`water-demo__overlay`,
		`water-demo__readout`,
		`Selena Surface`,
		`splash kernel`,
		`rotationY={-0.45}`,
		`scaleX={0.018}`,
		`scaleY={0.018}`,
		`scaleZ={0.018}`,
	} {
		if strings.Contains(page, unwanted) {
			t.Fatalf("page.gsx contains non-upstream overlay marker %q", unwanted)
		}
	}
	// The retired hand-written Elio/Selena source-identity/WGSL props (and the
	// two <Material> blocks' generic shaderSource/shaderSourceFiles) must not
	// resurface now that Selena is the sole primary WGSL source.
	for _, stale := range []string{
		`computeSource={data.waterComputeSource}`,
		`materialSource={data.waterMaterialSource}`,
		`computeSourceFiles={data.waterComputeSourceFiles}`,
		`materialSourceFiles={data.waterMaterialSourceFiles}`,
		`seedWGSL={data.waterSeedWGSL}`,
		`dropWGSL={data.waterDropWGSL}`,
		`displacementWGSL={data.waterDisplacementWGSL}`,
		`simulationWGSL={data.waterSimulationWGSL}`,
		`normalWGSL={data.waterNormalWGSL}`,
		`causticsWGSL={data.waterCausticsWGSL}`,
		`poolVertexWGSL={data.waterPoolVertexWGSL}`,
		`poolFragmentWGSL={data.waterPoolFragmentWGSL}`,
		`surfaceVertexWGSL={data.waterSurfaceVertexWGSL}`,
		`surfaceFragmentWGSL={data.waterSurfaceFragmentWGSL}`,
		`surfaceBelowFragmentWGSL={data.waterSurfaceBelowFragmentWGSL}`,
		`objectShadowWGSL={data.waterObjectShadowWGSL}`,
		`objectMeshShadowVertexWGSL={data.waterObjectMeshShadowVertexWGSL}`,
		`objectMeshShadowFragmentWGSL={data.waterObjectMeshShadowFragmentWGSL}`,
		`shaderSource={data.waterObjectMaterialSource}`,
		`shaderSourceFiles={data.waterObjectMaterialSourceFiles}`,
		`shaderSource={data.waterDuckMaterialSource}`,
		`shaderSourceFiles={data.waterDuckMaterialSourceFiles}`,
	} {
		if strings.Contains(page, stale) {
			t.Fatalf("page.gsx still wires retired hand-written prop %q", stale)
		}
	}
	for _, asset := range []string{"xpos.jpg", "xneg.jpg", "ypos.jpg", "zpos.jpg", "zneg.jpg"} {
		path := filepath.Join(dir, "../../../public/water", asset)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("missing upstream cubemap asset %s: %v", asset, err)
		}
		if info.Size() == 0 {
			t.Fatalf("upstream cubemap asset %s is empty", asset)
		}
	}
	if strings.Contains(page, `src="/water-controls.js"`) {
		t.Fatalf("page.gsx still references route-specific water-controls.js")
	}
	if strings.Contains(page, `<script`) {
		t.Fatalf("page.gsx still contains bespoke JavaScript; the water demo must be authored entirely through GoSX")
	}
	for _, bad := range []string{
		`name="gravity" checked={true}`,
		`name="densityEnabled" checked={true}`,
	} {
		if strings.Contains(page, bad) {
			t.Fatalf("water controls should match upstream unchecked defaults, found %q", bad)
		}
	}
	for _, want := range []string{
		".water-demo__scene {\n  position: absolute !important;\n  top: 0 !important;\n  right: min(20rem, calc(100vw - 3.5rem)) !important;",
		".water-demo__help[data-gosx-scene3d-panel-open=\"false\"] {\n  transform: translateX(0);",
		".water-demo__help-toggle {\n  position: absolute;",
		"display: none;",
		"@media (max-width: 720px)",
		".water-demo__scene {\n    right: 0 !important;\n    width: 100% !important;",
		".water-demo__help[data-gosx-scene3d-panel-open=\"false\"] {\n    transform: translateX(100%);",
		".water-demo__help-toggle {\n    display: grid;",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("page.css missing upstream-style responsive help panel contract %q", want)
		}
	}
	for _, stale := range []string{
		`pool-north-wall`,
		`pool-south-wall`,
		`pool-west-wall`,
		`pool-east-wall`,
		`pool-rim-lines`,
		`pool-wall`,
		`pool-rim`,
		`data.poolRimPoints`,
		`data.poolRimSegments`,
	} {
		if strings.Contains(page, stale) {
			t.Fatalf("page.gsx still contains route-authored pool geometry %q", stale)
		}
	}
	for _, want := range []string{
		"bindSceneManagedControlForms",
		"registerSceneManagedControlProfile",
		"sceneManagedControlProfiles",
		"sceneManagedControlData",
		"sceneManagedControlScope",
		"sceneManagedControlBindDisclosure",
		"sceneManagedControlBindPanelToggles",
		"data-gosx-scene3d-control-toggle",
		"data-gosx-scene3d-control-open",
		"data-gosx-scene3d-panel-toggle",
		"data-gosx-scene3d-panel-open",
		"data-gosx-scene3d-control-scope",
		"data-gosx-scene3d-panel-scope",
		"data-gosx-scene3d-active-panel",
		"sceneManagedFluidObjectProfile",
		"data-gosx-scene3d-control-data",
		"data-gosx-scene3d-control-data-ref",
		"SCENE_CMD_SET_PARTICLES",
		"SCENE_CMD_SET_MODELS",
		"applyCommands",
		"objectDisplacementScale",
		"objectDisplacementSpheres",
		"sceneManagedFluidObjectControlState",
		"sceneManagedFluidObjectQueueObjectDisplacementEvent",
		"objectDisplacementEvents",
		"sceneManagedFluidObjectPoolKey",
		"controlState.poolKey = poolKey",
		"sceneManagedFluidObjectObserveSelection",
		"sceneManagedFluidObjectInactiveY",
		"sceneManagedFluidObjectStepPhysics",
		"sceneManagedFluidObjectPhysicsSettings",
		"sceneManagedFluidObjectInteractionSettings",
		"SCENE_MANAGED_FLUID_OBJECT_MIN_STRAIGHT_POOL_EDGE",
		"sceneManagedFluidObjectMaxCornerRadius",
		"sceneManagedFluidObjectClampCornerRadius",
		"cornerField.max = String(maxCornerRadius)",
		"form.dataset.maxCornerRadius = String(rounded ? maxCornerRadius : 0)",
		"sceneManagedFluidObjectQueueDrop",
		"sceneManagedFluidObjectCameraLightDirection",
		"sceneManagedFluidObjectSyncLightDirection",
		"return { x: 2, y: 2, z: -1 };",
		"next.lightDirectionY = sceneNumber(lightDirection.y, 2);",
		"data-gosx-scene3d-fluid-light-y",
		"sceneNumber(light.y, 2)",
		"settingLightDirection",
		"sceneManagedFluidObjectApply(form, sceneState, applyCommands, options)",
		"sceneManagedFluidObjectInteractionProfile",
		"data-gosx-scene3d-interaction-profile",
		"water-object-drop-orbit",
		"sceneManagedFluidObjectStartInteraction",
		"sceneManagedFluidObjectDragObject",
		"sceneRayIntersectPlane",
		"pointerMode = \"MoveObject\"",
		"pointerMode = \"AddDrops\"",
		"pointerMode = \"OrbitCamera\"",
		"dropEventID",
		"dropEventRadius",
		"dropEventStrength",
		"data-gosx-scene3d-fluid-drop-events",
		"event.code === \"Space\"",
		"event.code === \"KeyG\"",
		"event.code === \"KeyL\"",
		"sceneManagedFluidObjectObjectStep",
		"poolChanged",
		"syncPoolPrevious",
		"objectPreviousSet",
		"objectPreviousY",
		"sceneManagedFluidObjectSetDisabled",
		"const physicsAvailable = active !== \"None\"",
		"form.dataset.physicsAvailable = String(physicsAvailable)",
		"form.dataset.densityOpen = String(physicsAvailable && controls.densityEnabled)",
		"if (field.disabled) return false",
		"form.dataset.gosxScene3dFluidObjectControlsReady",
		"data-gosx-scene3d-fluid-object-controls-ready",
		"paused",
	} {
		if !strings.Contains(runtimeSource, want) {
			t.Fatalf("managed water controls runtime missing %q", want)
		}
	}
	for _, want := range []string{
		// seedWGSL..objectMeshShadowFragmentWGSL remain as the field names the
		// retained bundle/manifest water-source diagnostic layer
		// (sceneWaterManifestShaderSources/sceneWaterShaderSourcesFromEntries/
		// sceneHydrateWaterEntriesFromSources/sceneWaterSurfaceSourceBytes)
		// still scans for -- see 16a-scene-webgpu.js's comment above
		// sceneWaterManifestShaderSources. They no longer drive any pipeline
		// decision (that "authored data-prop WGSL" resolution tier has been
		// removed; see sceneWaterSystemSignature's comment).
		"seedWGSL",
		"dropWGSL",
		"displacementWGSL",
		"simulationWGSL",
		"normalWGSL",
		"causticsWGSL",
		"poolVertexWGSL",
		"poolFragmentWGSL",
		"waterAuthoredComputeDispatches",
		"data-gosx-scene3d-webgpu-water-authored-compute-dispatches",
		"waterAuthoredCausticPasses",
		"data-gosx-scene3d-webgpu-water-authored-caustic-passes",
		"data-gosx-scene3d-webgpu-water-authored-pool-passes",
		"data-gosx-scene3d-webgpu-water-authored-pool-fragment-passes",
		"surfaceFragmentWGSL",
		"surfaceVertexWGSL",
		"surfaceBelowFragmentWGSL",
		"objectShadowWGSL",
		"objectMeshShadowVertexWGSL",
		"objectMeshShadowFragmentWGSL",
		"data-gosx-scene3d-webgpu-water-authored-object-shadow-passes",
		"data-gosx-scene3d-webgpu-water-authored-object-mesh-shadow-passes",
		"objectTextureMatrixBuffer",
		"writeWaterObjectTextureMatrices",
		"objectReflectionViewProjectionMatrix",
		"binding: 8",
		"sceneSelenaLiveTextureView",
		"sceneSelenaLiveBuffer",
		"sceneSelenaStorageBufferDescriptors",
		"gosx:",
		"waterAuthoredSurfaceDrawCalls",
		"data-gosx-scene3d-webgpu-water-authored-surface-draw-calls",
		"data-gosx-scene3d-webgpu-water-authored-surface-vertex-draw-calls",
	} {
		if !strings.Contains(webgpuSource, want) {
			t.Fatalf("Scene3D WebGPU water runtime missing %q", want)
		}
	}
	// The hand-written data-prop-authored pipeline-resolution tier itself
	// (as opposed to the retained diagnostic field names above) has been
	// fully removed: Selena resolves first, falling through directly to the
	// builtin SCENE_WATER_*_SOURCE pipelines with no more per-entry
	// "authored WGSL" pipeline builder in between.
	for _, stale := range []string{
		"function sceneWaterAuthoredShaderSource",
		"function sceneWaterAuthoredComputePipeline",
		"function sceneWaterAuthoredCausticsPipeline",
		"function sceneWaterAuthoredPoolVertexSource",
		"function sceneWaterAuthoredPoolFragmentSource",
		"function sceneWaterAuthoredSurfaceVertexSource",
		"function sceneWaterAuthoredSurfaceFragmentSource",
		"function sceneWaterAuthoredObjectShadowPipeline",
		"function sceneWaterAuthoredObjectMeshShadowPipeline",
		"function sceneWaterWithAuthoredShaderFallback",
	} {
		if strings.Contains(webgpuSource, stale) {
			t.Fatalf("Scene3D WebGPU water runtime still defines retired authored-pipeline function %q", stale)
		}
	}
	for _, stale := range []string{
		"SCENE_MANAGED_WATER_OBJECTS",
		"sceneManagedFluidObjectTorusKnotDisplacementSpheres",
		"sceneManagedFluidObjectDuckDisplacementSpheres",
		"/water/models/duck/Duck.gltf",
		`objectKind: "compound"`,
		"float-duck",
		"config.objectY - 0.08",
		"controls.densityEnabled ? controls.density",
		"sceneManagedFluidObjectRoundedFloorVertices",
		"sceneManagedFluidObjectRoundedWallVertices",
		"pool-rounded-floor",
		"pool-rounded-walls",
		"water-surface",
		"caustic-floor",
		"foam-rings",
		"pool-north-wall",
		"pool-south-wall",
		"pool-west-wall",
		"pool-east-wall",
		"pool-rim-lines",
		"controls.followCamera ? -0.45 : 2",
		`"rotationY":-0.45`,
		`"scaleX":0.018`,
		`"scaleY":0.018`,
		`"scaleZ":0.018`,
		"return { x: 2, y: 3, z: -1 };",
		"next.lightDirectionY = sceneNumber(lightDirection.y, 3);",
		"sceneNumber(light.y, 3)",
		`sceneManagedFluidObjectSetChecked(form, "followCamera", true)`,
		`sceneManagedFluidObjectSetChecked(form, "followCamera", false)`,
	} {
		if strings.Contains(runtimeSource, stale) {
			t.Fatalf("managed controls runtime still hardcodes water profile data %q", stale)
		}
	}
	if strings.Contains(runtimeSource, "bindsceneManagedFluidObjectControls") {
		t.Fatalf("managed controls runtime still exports water-specific bootstrap binder")
	}
	if strings.Contains(runtimeSource, "sceneManagedWater") ||
		strings.Contains(runtimeSource, `registerSceneManagedControlProfile("water"`) {
		t.Fatalf("managed controls runtime still exposes water-specific control profile symbols")
	}
	if strings.Contains(runtimeSource, `data-gosx-scene3d-water-controls"`) ||
		strings.Contains(runtimeSource, `data-gosx-scene3d-water-controls]`) {
		t.Fatalf("managed controls runtime still accepts the old water-specific form hook")
	}
	if strings.Contains(runtimeSource, `data-water-`) {
		t.Fatalf("managed controls runtime still depends on route-specific data-water attributes")
	}
	for _, want := range []string{
		"waterControlDataJSON",
		"torusKnotDisplacementSpheres",
		"duckDisplacementSpheres",
		"/water/models/duck/Duck.gltf",
		`"bounds": 0.5`,
		`"fit": "contain"`,
		`"fitAlign": "center-min-y"`,
	} {
		if !strings.Contains(program, want) {
			t.Fatalf("program.go missing %q", want)
		}
	}
	for _, stale := range []string{
		"const waterElio",
		"const waterSelena",
		"const waterObjectMaterialWGSL",
		"const waterDuckMaterialWGSL",
		// The hand-written Elio/Selena shader trees, their embed, and every
		// Go identifier that used to read them into WaterDemoData have been
		// fully retired -- Selena (selena_glsl.go, shaders/jeantimex-water.selena/)
		// is the sole primary WGSL source now.
		"waterShaderSources",
		"waterShaderSourceFiles",
		"waterComputeSourceFiles",
		"waterMaterialSourceFiles",
		"waterObjectMaterialSourceFiles",
		"waterDuckMaterialSourceFiles",
		"waterComputeSourceID",
		"waterMaterialSourceID",
		"waterObjectMaterialSourceID",
		"waterDuckMaterialSourceID",
		"go:embed shaders/jeantimex-water",
		"shaders/jeantimex-water.elio/",
		"shaders/jeantimex-water.sel/",
		"waterObjectMaterialLayout",
		"waterObjectMaterialUniforms",
		"waterDuckMaterialLayout",
		"waterDuckMaterialUniforms",
		`"pool-rounded-floor"`,
		`"pool-rounded-walls"`,
		"roundedPoolFloorGeometry",
		"roundedPoolWallGeometry",
		"dropletKernelWGSL",
		"causticPrismProps",
		"materialAttrs",
		"poolRimPoints",
		"poolRimSegments",
		`"rotationY":-0.45`,
		`"scaleX":0.018`,
		`"scaleY":0.018`,
		`"scaleZ":0.018`,
	} {
		if strings.Contains(program, stale) {
			t.Fatalf("program.go still contains route-side water approximation %q", stale)
		}
	}
}

// TestWaterSelenaGLESSlots verifies the demo compiles its Selena-authored water
// shaders only to the WebGL2 dialect, that those slots flow end-to-end into a
// WaterSystemIR, and that the per-shader Selena
// descriptor carries the cube texture, feedback statefield, and std140 uniform
// array bindings a WebGL2 runtime needs. Desktop GLSL remains a framework IR
// capability but is deliberately absent from this page's data payload.
func TestWaterSelenaGLESSlots(t *testing.T) {
	data, err := WaterDemoData()
	if err != nil {
		t.Fatalf("WaterDemoData returned error: %v", err)
	}

	// Every GLES slot page.gsx feeds must be present and look like WebGL2, while
	// unused desktop GLSL keys must not be generated.
	for _, p := range []string{
		"waterSeed", "waterDrop", "waterDisplacement", "waterSimulation", "waterNormal",
		"waterCaustics", "waterPool", "waterSurface", "waterSurfaceBelow",
		"waterObjectShadow", "waterCompoundShadow", "waterObjectMeshShadow",
	} {
		vGLES, _ := data[p+"VertexGLES"].(string)
		fGLES, _ := data[p+"FragmentGLES"].(string)
		if _, ok := data[p+"VertexGLSL"]; ok {
			t.Fatalf("%s unexpectedly compiled desktop vertex GLSL", p)
		}
		if _, ok := data[p+"FragmentGLSL"]; ok {
			t.Fatalf("%s unexpectedly compiled desktop fragment GLSL", p)
		}
		if strings.TrimSpace(vGLES) == "" || strings.TrimSpace(fGLES) == "" {
			t.Fatalf("%s GLES vertex/fragment empty (vtx=%d frag=%d)", p, len(vGLES), len(fGLES))
		}
		// GLES target is GLSL ES 3.00 (the WebGL2 dialect).
		if !strings.HasPrefix(strings.TrimSpace(vGLES), "#version 300 es") {
			t.Fatalf("%s GLES vertex is not #version 300 es: %.40q", p, vGLES)
		}
	}

	// The GLES surface fragment is the headline WebGL2 fallback shader.
	surfaceFrag, _ := data["waterSurfaceFragmentGLES"].(string)
	if !strings.HasPrefix(strings.TrimSpace(surfaceFrag), "#version 300 es") || !strings.Contains(surfaceFrag, "void main") {
		t.Fatalf("surface fragment GLES is invalid")
	}
	surfaceSource, err := waterSelenaFS.ReadFile("shaders/jeantimex-water.selena/surface.sel")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(surfaceSource), "var knot : array") || strings.Contains(string(surfaceSource), "var rfTorus") {
		t.Fatal("surface shader regressed to per-fragment analytic torus construction; complex objects must use projected mesh targets")
	}
	if !strings.Contains(string(surfaceSource), "meshTextureEnable") || !strings.Contains(string(surfaceSource), "objectRefractionTex") {
		t.Fatal("surface shader lost the projected complex-object optics path")
	}
	for _, want := range []string{"param cornerRadius", "param poolShape", "roundedPoolDistance", "discard"} {
		if !strings.Contains(string(surfaceSource), want) {
			t.Fatalf("surface shader lost rounded-pool coverage %q", want)
		}
	}
	normalFrag, _ := data["waterNormalFragmentGLES"].(string)
	if !strings.Contains(normalFrag, "cellSizeX > 0.000001") || !strings.Contains(normalFrag, "0.0078125") {
		t.Fatalf("normal fragment must keep legacy/unbound cell spacing finite")
	}

	descriptors, ok := data["waterShaderDescriptors"].(map[string]json.RawMessage)
	if !ok {
		t.Fatalf("waterShaderDescriptors = %T, want map[string]json.RawMessage", data["waterShaderDescriptors"])
	}

	// Decode the surface descriptor and assert the bindings the WebGL2 runtime
	// will need: a cube-map texture, a feedback statefield, and a std140 array.
	var surfaceLayout bindings.Layout
	if err := json.Unmarshal(descriptors["surface"], &surfaceLayout); err != nil {
		t.Fatalf("decode surface descriptor: %v", err)
	}
	var hasCube bool
	for _, tex := range surfaceLayout.Textures {
		if tex.Dimension == "cube" {
			hasCube = true
		}
	}
	if !hasCube {
		t.Fatalf("surface descriptor missing cube texture: %+v", surfaceLayout.Textures)
	}
	if len(surfaceLayout.States) == 0 {
		t.Fatalf("surface descriptor missing feedback statefield")
	}
	var hasArray bool
	for _, f := range surfaceLayout.UniformBlock.Fields {
		if f.Count > 1 {
			hasArray = true
		}
	}
	if !hasArray {
		t.Fatalf("surface descriptor missing std140 uniform array")
	}

	// The seed (feedback) descriptor must carry the ping-pong statefield + grid.
	var seedLayout bindings.Layout
	if err := json.Unmarshal(descriptors["seed"], &seedLayout); err != nil {
		t.Fatalf("decode seed descriptor: %v", err)
	}
	if seedLayout.Kind != bindings.SurfaceKindFeedback {
		t.Fatalf("seed descriptor kind = %q, want feedback", seedLayout.Kind)
	}
	if len(seedLayout.States) == 0 || seedLayout.Grid == nil {
		t.Fatalf("seed descriptor missing feedback states/grid: states=%v grid=%v", seedLayout.States, seedLayout.Grid)
	}

	// The compoundShadow descriptor is the WebGL2-only compound-object
	// (TorusKnot/Duck) footprint shadow pass. It must exist and carry the
	// `spheres` std140 array uniform (this is the array-param support that
	// motivated lifting post-kind materials' CodeUnsupportedType restriction
	// for array params — object-shadow.sel could not express this).
	compoundShadowRaw, ok := descriptors["compoundShadow"]
	if !ok {
		t.Fatalf("waterShaderDescriptors missing compoundShadow entry")
	}
	var compoundShadowLayout bindings.Layout
	if err := json.Unmarshal(compoundShadowRaw, &compoundShadowLayout); err != nil {
		t.Fatalf("decode compoundShadow descriptor: %v", err)
	}
	var hasSphereArray bool
	for _, f := range compoundShadowLayout.UniformBlock.Fields {
		if f.Name == "spheres" && f.Count > 1 {
			hasSphereArray = true
		}
	}
	if !hasSphereArray {
		t.Fatalf("compoundShadow descriptor missing spheres array uniform: %+v", compoundShadowLayout.UniformBlock.Fields)
	}

	// End-to-end: real GLES + descriptors flow into WaterSystemIR. Sentinel
	// desktop GLSL proves the framework-wide IR fields remain available even
	// though WaterDemoData no longer compiles them.
	ws := scene.WaterSystem{
		ID:                  "water-main",
		SurfaceVertexGLSL:   "attribute vec3 position; void main(){gl_Position=vec4(position,1.0);}",
		SurfaceFragmentGLSL: "void main(){gl_FragColor=vec4(1.0);}",
		SurfaceVertexGLES:   data["waterSurfaceVertexGLES"].(string),
		SurfaceFragmentGLES: data["waterSurfaceFragmentGLES"].(string),
		ShaderDescriptors:   descriptors,
	}
	ir := (scene.Props{Graph: scene.NewGraph(ws)}).SceneIR()
	if len(ir.WaterSystems) != 1 {
		t.Fatalf("expected one water system, got %d", len(ir.WaterSystems))
	}
	got := ir.WaterSystems[0]
	if got.SurfaceFragmentGLSL != ws.SurfaceFragmentGLSL {
		t.Fatalf("WaterSystemIR.SurfaceFragmentGLSL did not preserve framework field")
	}
	if string(got.ShaderDescriptors["surface"]) != string(descriptors["surface"]) {
		t.Fatalf("WaterSystemIR.ShaderDescriptors[surface] did not round-trip")
	}
}
