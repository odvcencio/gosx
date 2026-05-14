package inspect

import (
	"testing"
)

func TestInspectJSONRepresentativeScene(t *testing.T) {
	data := []byte(`{
		"schema": "gosx.scene3d.ir.v1",
		"objects": [
			{
				"id": "hero-box",
				"kind": "cube",
				"texture": "/assets/albedo.png",
				"normalMap": "/assets/normal.png"
			},
			{
				"id": "warning-torus",
				"kind": "torus",
				"radius": 1,
				"tube": 1.25,
				"customVertexWGSL": "@vertex fn main() {}"
			}
		],
		"points": [
			{
				"id": "stars",
				"count": 2,
				"positions": [0, 0, 0, 1, 1, 1],
				"sizes": [1, 2],
				"colors": ["#fff", "#ccd"]
			}
		],
		"instancedMeshes": [
			{
				"id": "field",
				"count": 2,
				"kind": "sphere",
				"segments": 8,
				"transforms": [
					1, 0, 0, 0,
					0, 1, 0, 0,
					0, 0, 1, 0,
					0, 0, 0, 1,
					1, 0, 0, 0,
					0, 1, 0, 0,
					0, 0, 1, 0,
					2, 0, 0, 1
				]
			}
		],
		"html": [
			{
				"id": "panel",
				"target": "hero-box",
				"mode": "texture",
				"html": "<button>Status</button>",
				"fallback": "<button>Status</button>",
				"fallbackReason": "accessible-dom",
				"textureKey": "panel-v1",
				"textureWidth": 512,
				"textureHeight": 256
			}
		],
		"lights": [
			{
				"id": "sun",
				"kind": "directional",
				"intensity": 1.5,
				"castShadow": true,
				"shadowSize": 256
			}
		],
		"postEffects": [
			{"kind": "bloom", "intensity": 0.4}
		],
		"postFXMaxPixels": 230400
	}`)

	report, err := InspectJSON("fixtures/representative.scene.json", data, Options{})
	if err != nil {
		t.Fatalf("InspectJSON returned error: %v", err)
	}

	if report.Path != "fixtures/representative.scene.json" {
		t.Fatalf("Path = %q, want fixture path", report.Path)
	}
	if report.Surface.ID != "representative.scene" {
		t.Fatalf("Surface.ID = %q, want representative.scene", report.Surface.ID)
	}
	if report.Surface.Objects != 2 {
		t.Fatalf("Surface.Objects = %d, want 2", report.Surface.Objects)
	}
	if report.Surface.InstancedMeshes != 1 {
		t.Fatalf("Surface.InstancedMeshes = %d, want 1", report.Surface.InstancedMeshes)
	}
	if report.Surface.Points != 1 {
		t.Fatalf("Surface.Points = %d, want 1", report.Surface.Points)
	}
	if report.Surface.HTML != 1 {
		t.Fatalf("Surface.HTML = %d, want 1", report.Surface.HTML)
	}
	if report.Surface.Lights != 1 {
		t.Fatalf("Surface.Lights = %d, want 1", report.Surface.Lights)
	}
	if report.Surface.PostEffects != 1 {
		t.Fatalf("Surface.PostEffects = %d, want 1", report.Surface.PostEffects)
	}
	if report.Surface.EstimatedDrawCalls != 6 {
		t.Fatalf("Surface.EstimatedDrawCalls = %d, want 6", report.Surface.EstimatedDrawCalls)
	}

	wantFeatures := []string{
		"geometry.cube",
		"geometry.torus",
		"geometry.instancedMesh",
		"geometry.sphere",
		"geometry.points",
		"html.texture",
		"lighting.directional",
		"lighting.shadows",
		"material.textureMap",
		"material.normalMap",
		"material.customWGSL",
		"postfx.bloom",
	}
	for _, key := range wantFeatures {
		if report.FeatureUse[key] == 0 {
			t.Fatalf("FeatureUse[%q] = 0, want non-zero; features = %#v", key, report.FeatureUse)
		}
	}

	if len(report.Fallbacks) != 1 {
		t.Fatalf("len(Fallbacks) = %d, want 1: %#v", len(report.Fallbacks), report.Fallbacks)
	}
	fallback := report.Fallbacks[0]
	if fallback.Feature != "html.texture" || fallback.ID != "panel" || fallback.Reason == "" {
		t.Fatalf("Fallbacks[0] = %#v, want html texture fallback for panel with reason", fallback)
	}

	if report.Assets.Textures != 2 {
		t.Fatalf("Assets.Textures = %d, want 2", report.Assets.Textures)
	}
	if report.Assets.HTMLTextureSurfaces != 1 {
		t.Fatalf("Assets.HTMLTextureSurfaces = %d, want 1", report.Assets.HTMLTextureSurfaces)
	}
	if report.Assets.Shaders != 1 {
		t.Fatalf("Assets.Shaders = %d, want 1", report.Assets.Shaders)
	}
	if got, want := report.Assets.Sources, []string{"/assets/albedo.png", "/assets/normal.png"}; !equalStrings(got, want) {
		t.Fatalf("Assets.Sources = %#v, want %#v", got, want)
	}

	if report.Memory.GeometryBytes == 0 {
		t.Fatalf("Memory.GeometryBytes = 0, want non-zero")
	}
	if report.Memory.InstanceBytes == 0 {
		t.Fatalf("Memory.InstanceBytes = 0, want non-zero")
	}
	if report.Memory.PointBytes == 0 {
		t.Fatalf("Memory.PointBytes = 0, want non-zero")
	}
	if report.Memory.TextureBytes == 0 {
		t.Fatalf("Memory.TextureBytes = 0, want non-zero")
	}
	if report.Memory.HTMLTextureBytes == 0 {
		t.Fatalf("Memory.HTMLTextureBytes = 0, want non-zero")
	}
	if report.Memory.ShadowBytes == 0 {
		t.Fatalf("Memory.ShadowBytes = 0, want non-zero")
	}
	if report.Memory.PostFXBytes == 0 {
		t.Fatalf("Memory.PostFXBytes = 0, want non-zero")
	}
	if report.Memory.TotalGPUBytes == 0 {
		t.Fatalf("Memory.TotalGPUBytes = 0, want non-zero")
	}

	if !report.Validation.Valid {
		t.Fatalf("Validation.Valid = false, diagnostics = %#v", report.Validation.Diagnostics)
	}
	if len(report.Validation.Diagnostics) != 1 {
		t.Fatalf("len(Validation.Diagnostics) = %d, want 1: %#v", len(report.Validation.Diagnostics), report.Validation.Diagnostics)
	}
	if report.Validation.Diagnostics[0].Code != "scene.primitive.torus_tube_large" {
		t.Fatalf("diagnostic code = %q, want scene.primitive.torus_tube_large", report.Validation.Diagnostics[0].Code)
	}
}

func TestParseBudgetDirectAndWrapped(t *testing.T) {
	direct, err := ParseBudget([]byte(`{
		"maxInitialGPUBytes": 1000,
		"maxDrawCalls": 12,
		"maxP95FrameMS": 16.7
	}`))
	if err != nil {
		t.Fatalf("ParseBudget direct returned error: %v", err)
	}
	if direct.MaxInitialGPUBytes != 1000 || direct.MaxDrawCalls != 12 || direct.MaxP95FrameMS != 16.7 {
		t.Fatalf("direct budget = %#v, want parsed limits", direct)
	}

	wrapped, err := ParseBudget([]byte(`{
		"scene3d": {
			"maxTextureBytes": 2048,
			"maxHTMLTextureBytes": 1024,
			"maxFirstFrameUploads": 4
		}
	}`))
	if err != nil {
		t.Fatalf("ParseBudget wrapped returned error: %v", err)
	}
	if wrapped.MaxTextureBytes != 2048 || wrapped.MaxHTMLTextureBytes != 1024 || wrapped.MaxFirstFrameUploads != 4 {
		t.Fatalf("wrapped budget = %#v, want nested scene3d limits", wrapped)
	}
}

func TestEvaluateBudgetAndBudgetFailed(t *testing.T) {
	scene := SceneReport{
		Path: "budget.scene.json",
		Surface: SurfaceReport{
			EstimatedDrawCalls:   4,
			EstimatedUploadCount: 3,
		},
		Memory: SceneMemoryEstimate{
			TextureBytes:     100,
			HTMLTextureBytes: 50,
			ShadowBytes:      20,
			PostFXBytes:      30,
			TotalGPUBytes:    200,
		},
	}

	passResults := EvaluateBudget(scene, SceneBudget{
		MaxInitialGPUBytes: 300,
		MaxTextureBytes:    200,
		MaxDrawCalls:       5,
	}, false)
	if len(passResults) != 3 {
		t.Fatalf("len(passResults) = %d, want 3: %#v", len(passResults), passResults)
	}
	for _, result := range passResults {
		if result.Status != BudgetPass {
			t.Fatalf("pass result for %s has status %q, want pass: %#v", result.Category, result.Status, passResults)
		}
	}
	if BudgetFailed(passResults, false) {
		t.Fatalf("BudgetFailed(passResults, false) = true, want false")
	}

	failResults := EvaluateBudget(scene, SceneBudget{
		MaxInitialGPUBytes: 199,
		MaxDrawCalls:       4,
	}, false)
	if !hasBudgetStatus(failResults, "initialGPUBytes", BudgetFail) {
		t.Fatalf("failResults missing initialGPUBytes failure: %#v", failResults)
	}
	if !BudgetFailed(failResults, false) {
		t.Fatalf("BudgetFailed(failResults, false) = false, want true")
	}

	unknownResults := EvaluateBudget(scene, SceneBudget{
		MaxP95FrameMS: 16,
	}, false)
	if !hasBudgetStatus(unknownResults, "p95FrameMS", BudgetUnknown) {
		t.Fatalf("unknownResults missing p95FrameMS unknown: %#v", unknownResults)
	}
	if BudgetFailed(unknownResults, false) {
		t.Fatalf("BudgetFailed(unknownResults, false) = true, want false")
	}
	if !BudgetFailed(unknownResults, true) {
		t.Fatalf("BudgetFailed(unknownResults, true) = false, want true")
	}
}

func hasBudgetStatus(results []BudgetResult, category string, status BudgetStatus) bool {
	for _, result := range results {
		if result.Category == category && result.Status == status {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
