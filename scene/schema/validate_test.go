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

func TestValidateJSONAcceptsAllMajorRecordsAndUnknownFields(t *testing.T) {
	report := ValidateJSON([]byte(`{
		"schema":"`+scene.SceneIRSchema+`",
		"unknownTopLevel":{"preserve":"forward-compatible"},
		"objects":[{"id":"cube-1","kind":"cube","size":1,"futureObjectField":true}],
		"models":[{"id":"model-1","src":"/model.glb","scaleX":1,"scaleY":1,"scaleZ":1}],
		"points":[{"id":"points-1","count":2,"positions":[0,0,0,1,1,1],"sizes":[1,2],"colors":["#fff","#000"],"positionStride":3}],
		"instancedMeshes":[{"id":"batch-1","count":1,"kind":"cube","transforms":[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1],"colors":["#fff"]}],
		"instancedGLBMeshes":[{"id":"glb-batch-1","src":"/part.glb","instances":[{"id":"glb-instance-1","x":1,"scaleX":1,"scaleY":1,"scaleZ":1}]}],
		"computeParticles":[{"id":"particles-1","count":8,"emitter":{"kind":"sphere","radius":1,"rate":2,"lifetime":3},"material":{"size":2,"opacity":1},"bounds":10}],
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
