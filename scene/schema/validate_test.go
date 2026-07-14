package schema

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

func TestValidateJSONAcceptsMinimalScene(t *testing.T) {
	report := ValidateJSON([]byte(`{
		"schema":"`+scene.SceneIRSchema+`",
		"objects":[{"id":"cube-1","kind":"cube","size":1}],
		"html":[{"id":"label","mode":"dom","html":"<button>Inspect</button>","fallback":"Inspect"}]
	}`), Options{Strict: true})
	if !report.Valid {
		t.Fatalf("expected valid report: %+v", report.Diagnostics)
	}
}

func TestJSONSchemaIncludesAuthoredWaterShaderFields(t *testing.T) {
	schemaText := string(JSONSchema())
	for _, field := range []string{
		"seedWGSL",
		"dropWGSL",
		"displacementWGSL",
		"simulationWGSL",
		"normalWGSL",
		"causticsWGSL",
		"poolVertexWGSL",
		"poolFragmentWGSL",
		"surfaceVertexWGSL",
		"surfaceFragmentWGSL",
		"surfaceBelowFragmentWGSL",
		"objectShadowWGSL",
		"objectMeshShadowVertexWGSL",
		"objectMeshShadowFragmentWGSL",
		"seedWGSLRef",
		"dropWGSLRef",
		"displacementWGSLRef",
		"simulationWGSLRef",
		"normalWGSLRef",
		"causticsWGSLRef",
		"poolVertexWGSLRef",
		"poolFragmentWGSLRef",
		"surfaceVertexWGSLRef",
		"surfaceFragmentWGSLRef",
		"surfaceBelowFragmentWGSLRef",
		"objectShadowWGSLRef",
		"objectMeshShadowVertexWGSLRef",
		"objectMeshShadowFragmentWGSLRef",
	} {
		if !strings.Contains(schemaText, `"`+field+`"`) {
			t.Fatalf("embedded schema missing water shader field %q", field)
		}
	}
}

func TestValidateJSONDetectsDuplicateIDs(t *testing.T) {
	report := ValidateJSON([]byte(`{
		"objects":[{"id":"dup","kind":"cube"},{"id":"dup","kind":"sphere"}]
	}`), Options{Strict: true})
	if report.Valid {
		t.Fatalf("expected invalid duplicate ID report")
	}
	if !hasCode(report, "scene.id.duplicate") {
		t.Fatalf("expected duplicate ID diagnostic: %+v", report.Diagnostics)
	}
}

func TestValidateJSONTextureHTMLRequiresFallbackInStrictMode(t *testing.T) {
	report := ValidateJSON([]byte(`{
		"html":[{"id":"panel","mode":"texture","html":"<input>","textureWidth":512,"textureHeight":256}]
	}`), Options{Strict: true})
	if report.Valid {
		t.Fatal("expected missing fallback to fail strict validation")
	}
	if !hasCode(report, "scene.html.texture_fallback") {
		t.Fatalf("expected texture fallback diagnostic: %+v", report.Diagnostics)
	}
}

func TestValidateJSONRejectsInvalidPrimitiveParameters(t *testing.T) {
	report := ValidateJSON([]byte(`{
		"objects":[{"id":"bad","kind":"sphere","radius":-1}]
	}`), Options{})
	if report.Valid {
		t.Fatal("expected negative radius to fail")
	}
	if !hasCode(report, "scene.primitive.invalid_parameter") {
		t.Fatalf("expected primitive parameter diagnostic: %+v", report.Diagnostics)
	}
}

func TestValidateJSONRejectsTextureOverBudget(t *testing.T) {
	report := ValidateJSON([]byte(`{
		"html":[{"id":"panel","mode":"texture","fallback":"Panel","textureWidth":128,"textureHeight":128}]
	}`), Options{MaxTexturePixels: 1024})
	if report.Valid {
		t.Fatal("expected texture budget failure")
	}
	if !hasCode(report, "scene.texture.over_budget") {
		t.Fatalf("expected texture budget diagnostic: %+v", report.Diagnostics)
	}
}

func TestValidateJSONRejectsNegativeModelBounds(t *testing.T) {
	report := ValidateJSON([]byte(`{
				"models":[{"id":"bad-model","src":"/bad.glb","bounds":-1}]
		}`), Options{})
	if report.Valid {
		t.Fatal("expected negative model bounds to fail")
	}
	if !hasCode(report, "scene.numeric.negative") {
		t.Fatalf("expected model bounds diagnostic: %+v", report.Diagnostics)
	}
}

func TestValidateJSONAcceptsAllMajorRecordsAndUnknownFields(t *testing.T) {
	report := ValidateJSON([]byte(`{
		"schema":"`+scene.SceneIRSchema+`",
		"unknownTopLevel":{"preserve":"forward-compatible"},
		"objects":[{"id":"cube-1","kind":"cube","size":1,"futureObjectField":true}],
		"models":[{"id":"model-1","src":"/model.glb","scaleX":1,"scaleY":1,"scaleZ":1,"bounds":0.5,"fit":"contain","fitAlign":"center-min-y"}],
		"points":[{"id":"points-1","count":2,"positions":[0,0,0,1,1,1],"sizes":[1,2],"colors":["#fff","#000"],"positionStride":3}],
		"instancedMeshes":[{"id":"batch-1","count":1,"kind":"cube","transforms":[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1],"colors":["#fff"]}],
		"instancedGLBMeshes":[{"id":"glb-batch-1","src":"/part.glb","instances":[{"id":"glb-instance-1","x":1,"scaleX":1,"scaleY":1,"scaleZ":1}]}],
		"computeParticles":[{"id":"particles-1","count":8,"emitter":{"kind":"sphere","radius":1,"rate":2,"lifetime":3},"material":{"size":2,"opacity":1},"bounds":10}],
		"waterSystems":[{"id":"water-1","interactionProfile":"water-object-drop-orbit","interactionTarget":"water-1","interactionObject":"Sphere","resolution":256,"surfaceResolution":201,"poolShape":"Box","poolWidth":7.2,"poolHeight":1.1,"poolLength":4.4,"seedDrops":20,"dropRadius":0.03,"dropEventID":1,"dropX":-0.1,"dropZ":0.2,"dropEventRadius":0.03,"dropEventStrength":0.01,"shallowColor":"#7ad1eb","deepColor":"#082e57","objectKind":"compound","objectX":-1.28,"objectY":0.22,"objectZ":0.1,"objectRadius":0.44,"objectBobAmplitude":0.08,"objectBobSpeed":1.55,"objectDisplacementScale":1,"objectDisplacementSpheres":[{"offsetX":0,"offsetY":0,"offsetZ":0,"radius":0.15},{"offsetX":0.1,"offsetY":0.05,"offsetZ":-0.02,"radius":0.08}],"computeBackend":"elio","materialBackend":"selena"}],
		"animations":[{"name":"move","duration":1,"channels":[{"targetNode":0,"property":"translation","times":[0,1],"values":[0,0,0,1,1,1]}]}],
		"labels":[{"id":"label-1","text":"Ready","maxWidth":160,"maxLines":2}],
		"sprites":[{"id":"sprite-1","src":"/sprite.png","width":24,"height":24,"scale":1,"opacity":1}],
		"html":[{"id":"html-1","target":"cube-1","mode":"dom","html":"<button>Inspect</button>","futureHTMLField":"ok"}],
		"lights":[{"id":"light-1","kind":"directional","intensity":1,"shadowSize":512}],
		"postEffects":[{"kind":"bloom","threshold":0.8}],
		"postFXMaxPixels":1048576,
		"shadowMaxPixels":1048576
	}`), Options{Strict: true})
	if !report.Valid {
		t.Fatalf("expected valid report: %+v", report.Diagnostics)
	}
}

func TestValidateJSONWarnsUnknownWaterInteractionProfile(t *testing.T) {
	report := ValidateJSON([]byte(`{
		"waterSystems":[{"id":"water-1","interactionProfile":"mystery-profile"}]
	}`), Options{})
	if !report.Valid {
		t.Fatalf("unknown interaction profile should warn but remain valid: %+v", report.Diagnostics)
	}
	if !hasCode(report, "scene.water.unknown_interaction_profile") {
		t.Fatalf("expected unknown interaction profile diagnostic: %+v", report.Diagnostics)
	}
}

func TestValidateJSONRejectsInvalidWaterSystem(t *testing.T) {
	report := ValidateJSON([]byte(`{
		"waterSystems":[{"id":"bad-water","resolution":-1,"surfaceResolution":-1,"seedDrops":-2,"dropEventID":-1,"poolWidth":-1,"dropRadius":-0.1,"dropEventRadius":-0.1,"causticsResolution":-1,"objectTextureResolution":-1,"objectTexturePixelBudget":-1,"objectShadowResolution":-1,"objectRadius":-1,"objectHalfSizeX":-0.1,"objectBobSpeed":-1,"objectDisplacementSpheres":[{"radius":-0.1}]}]
	}`), Options{})
	if report.Valid {
		t.Fatal("expected invalid water system to fail")
	}
	for _, code := range []string{
		"scene.water.invalid_resolution",
		"scene.water.invalid_seed_drops",
		"scene.water.invalid_drop_event_id",
		"scene.water.invalid_texture_resolution",
		"scene.numeric.negative",
	} {
		if !hasCode(report, code) {
			t.Fatalf("expected diagnostic %s: %+v", code, report.Diagnostics)
		}
	}
}

func TestValidateJSONRejectsNegativeHDRWaterColor(t *testing.T) {
	report := ValidateJSON([]byte(`{"waterSystems":[{"id":"bad-water-color","aboveWaterColorB":-0.1}]}`), Options{})
	if report.Valid || !hasCode(report, "scene.numeric.negative") {
		t.Fatalf("expected negative HDR water color diagnostic: %+v", report.Diagnostics)
	}
}

func TestValidateJSONDetectsDuplicateInstanceIDsGlobally(t *testing.T) {
	report := ValidateJSON([]byte(`{
		"objects":[{"id":"shared","kind":"cube"}],
		"instancedGLBMeshes":[{"id":"glb","src":"/part.glb","instances":[{"id":"shared"}]}]
	}`), Options{})
	if report.Valid {
		t.Fatalf("expected invalid duplicate ID report")
	}
	if !hasCode(report, "scene.id.duplicate") {
		t.Fatalf("expected duplicate ID diagnostic: %+v", report.Diagnostics)
	}
}

func TestValidateJSONHardensReferencesArraysCompressionAndAnimations(t *testing.T) {
	report := ValidateJSON([]byte(`{
		"objects":[{"id":"cube","kind":"cube"}],
		"points":[{"id":"bad-points","count":2,"positions":[0,0,0,1],"sizes":[1],"compressedPositions":[{"dim":-1,"bitWidth":-1,"count":-1}]}],
		"instancedMeshes":[{"id":"bad-batch","count":2,"kind":"cube","transforms":[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1],"colors":["#fff"]}],
		"computeParticles":[{"id":"bad-particles","count":1,"bounds":-1,"emitter":{"radius":-1,"arms":-2},"material":{"size":-1}}],
		"html":[{"id":"bad-html","target":"missing","mode":"dom","html":"<div></div>"}],
		"animations":[{"name":"bad-animation","duration":-1,"channels":[{"targetNode":8,"property":"","times":[0,0.5,0.25]}]}],
		"postFXMaxPixels":-1,
		"shadowMaxPixels":-1
	}`), Options{})
	if report.Valid {
		t.Fatalf("expected invalid report")
	}
	for _, code := range []string{
		"scene.points.invalid_positions",
		"scene.points.count_mismatch",
		"scene.compressed.invalid_metadata",
		"scene.instances.count_mismatch",
		"scene.numeric.negative",
		"scene.particles.invalid_emitter",
		"scene.html.invalid_target",
		"scene.animation.invalid_target",
		"scene.animation.invalid_time",
		"scene.animation.values_missing",
		"scene.postfx.invalid_max_pixels",
		"scene.shadow.invalid_max_pixels",
	} {
		if !hasCode(report, code) {
			t.Fatalf("expected diagnostic %s: %+v", code, report.Diagnostics)
		}
	}
}

func TestValidateJSONPostEffectsRequireKindOrType(t *testing.T) {
	report := ValidateJSON([]byte(`{
		"postEffects":[{"threshold":0.8},{"type":"custom"}]
	}`), Options{})
	if report.Valid {
		t.Fatal("expected post effect without kind/type to fail")
	}
	if !hasCode(report, "scene.post_effect.kind_missing") {
		t.Fatalf("expected post effect kind diagnostic: %+v", report.Diagnostics)
	}
}

func TestJSONSchemaEmbedded(t *testing.T) {
	data := JSONSchema()
	if len(data) == 0 {
		t.Fatal("embedded schema is empty")
	}
	if !strings.Contains(string(data), `"GoSX Scene3D IR"`) {
		t.Fatalf("embedded schema does not look like Scene3D schema: %.80q", data)
	}
}

func hasCode(report Report, code string) bool {
	for _, diag := range report.Diagnostics {
		if diag.Code == code {
			return true
		}
	}
	return false
}
