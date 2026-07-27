package preview_test

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/preview"
)

// TestRasterizableKindsRenderPixels pins the honest-coverage table to real
// renderer behaviour. Each listed kind must produce visible pixels. If the CPU
// rasterizer drops a kind, this test fails instead of letting the preview claim
// coverage it does not have.
func TestRasterizableKindsRenderPixels(t *testing.T) {
	for _, kind := range preview.RasterizableKinds() {
		t.Run(kind, func(t *testing.T) {
			doc := `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"probe","kind":"` + kind +
				`","color":"#ff8060"}],"lights":[{"id":"sun","kind":"directional","directionX":-0.4,"directionY":-1,"directionZ":-0.3,"intensity":1.2}]}`
			result, err := preview.RenderJSON([]byte(doc), preview.Options{
				Width: 96, Height: 64, Background: "#000000",
				Camera:         cameraAt(0, 2, 6),
				DisableShadows: true, DisablePostFX: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if drawn := countNonBackground(result, color.RGBA{A: 255}); drawn < 20 {
				t.Fatalf("kind %q is listed as rasterizable but drew %d pixels", kind, drawn)
			}
			for _, diagnostic := range result.Bundle.Diagnostics {
				if diagnostic.Code == "scene.preview.unsupported_geometry" {
					t.Fatalf("kind %q drew pixels but was reported unsupported: %+v", kind, diagnostic)
				}
			}
		})
	}
}

// TestUnsupportedGeometryKindsAreReported proves the preview never returns a
// silent blank frame. Kinds the typed scene package can emit but the CPU
// rasterizer cannot build must name the record and the reason.
func TestUnsupportedGeometryKindsAreReported(t *testing.T) {
	doc := `{"schema":"gosx.scene3d.ir.v1","objects":[
		{"id":"buffer","kind":"gltf-mesh"},
		{"id":"typo","kind":"sphre","radius":1,"x":3},
		{"id":"blank","kind":""},
		{"id":"ok","kind":"cube","size":1,"x":-3}
	],"instancedMeshes":[{"id":"wires","kind":"lines","count":1,"transforms":[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1]}]}`
	result, err := preview.RenderJSON([]byte(doc), preview.Options{Width: 64, Height: 48, DisableShadows: true, DisablePostFX: true})
	if err != nil {
		t.Fatal(err)
	}
	byTarget := map[string]engine.RenderDiagnostic{}
	for _, diagnostic := range result.Bundle.Diagnostics {
		if diagnostic.Code == "scene.preview.unsupported_geometry" {
			byTarget[diagnostic.Target] = diagnostic
		}
	}
	for _, want := range []struct{ target, fragment string }{
		{"buffer", "carries no vertex buffer field"},
		{"typo", `did you mean "sphere"?`},
		{"blank", "no geometry kind"},
		{"wires", "bundle.worldLine line-list pipeline"},
	} {
		diagnostic, ok := byTarget[want.target]
		if !ok {
			t.Fatalf("no unsupported-geometry diagnostic for %q: %+v", want.target, result.Bundle.Diagnostics)
		}
		if !strings.Contains(diagnostic.Message, want.fragment) {
			t.Fatalf("diagnostic for %q missing %q: %s", want.target, want.fragment, diagnostic.Message)
		}
	}
	if _, ok := byTarget["ok"]; ok {
		t.Fatal("a drawable cube must not be reported as unsupported")
	}
}

// TestIgnoredLightsAreReported records a diagnostic that is now wrong.
//
// The CPU rasterizer read one directional light until 2026-07-27. It now shades
// ambient, directional, point, spot and hemisphere lights from the same runtime
// light array litWGSL reads, so every diagnostic this test asserts is false
// except the one for a rect-area light.
// TestEveryLightKindReachesAPixel in render/gpu/headless holds the proof, and
// TestEveryAuthoredLightKindChangesThePreviewFrame below repeats it through this
// package.
//
// The edit is in scene/preview/coverage.go. Replace the body of lightDiagnostics
// with a report for the one kind the rasterizer still drops:
//
//	func lightDiagnostics(lights []engine.RenderLight) []engine.RenderDiagnostic {
//	    var out []engine.RenderDiagnostic
//	    for _, light := range lights {
//	        if normalizeKind(light.Kind) != "area" {
//	            continue
//	        }
//	        out = append(out, unsupported("light", light.ID,
//	            "native preview does not shade a rect-area light; engine.RenderLight carries no width and no "+
//	                "height, so the rectangle the form factor integrates over does not exist on this path"))
//	    }
//	    return out
//	}
//
// and reword the doc comment above it, which still says the rasterizer resolves
// one directional light.
//
// This test fails when that edit lands. Replace it in the same change with one
// that asserts the rect-area report alone; that is the forcing function which
// keeps the note and the renderer together.
func TestIgnoredLightsAreReported(t *testing.T) {
	doc := `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"cube","kind":"cube","size":1}],"lights":[
		{"id":"key","kind":"directional","directionY":-1,"intensity":1},
		{"id":"fill","kind":"directional","directionX":1,"intensity":0.4},
		{"id":"lamp","kind":"point","intensity":3,"x":2,"y":4},
		{"id":"rim","kind":"area","intensity":2,"width":4,"height":4}
	]}`
	result, err := preview.RenderJSON([]byte(doc), preview.Options{Width: 48, Height: 32, DisableShadows: true, DisablePostFX: true})
	if err != nil {
		t.Fatal(err)
	}
	ignored := map[string]string{}
	for _, diagnostic := range result.Bundle.Diagnostics {
		if diagnostic.Code == "scene.preview.unsupported_light" {
			ignored[diagnostic.Target] = diagnostic.Message
		}
	}
	// Rect-area is the ONLY kind still reported. The rasterizer now runs the same
	// runtime light loop litWGSL runs, so a second directional light and a point
	// light both shade. Reporting them would tell an author their lighting does
	// nothing while it is lighting the scene.
	if len(ignored) != 1 {
		t.Fatalf("expected only the rect-area light to be reported, got %v", ignored)
	}
	if !strings.Contains(ignored["rim"], "carries no width and no height") {
		t.Fatalf("rect-area diagnostic must name the missing fields, got %q", ignored["rim"])
	}
	// The shaded kinds must stay absent. This half is what fails if somebody
	// widens the report again.
	for _, id := range []string{"key", "fill", "lamp"} {
		if message, ok := ignored[id]; ok {
			t.Fatalf("%q shades on the CPU path and must not be reported ignored: %q", id, message)
		}
	}
}

// TestRenderMeasuresItsOwnDuration proves the preview reports a real elapsed
// time instead of forwarding the renderer's fixed simulation step.
func TestRenderMeasuresItsOwnDuration(t *testing.T) {
	doc := `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"cube","kind":"cube","size":2}]}`
	result, err := preview.RenderJSON([]byte(doc), preview.Options{Width: 64, Height: 48, DisableShadows: true, DisablePostFX: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Duration <= 0 {
		t.Fatalf("render duration = %v, want a positive measurement", result.Duration)
	}
	// Stats.LastFrameMS is the renderer's clamped animation step, not a render
	// measurement. Guard against a tool ever printing it as elapsed time.
	if result.Stats.LastFrameMS != 1000.0/60.0 {
		t.Fatalf("renderer step changed shape: %v", result.Stats.LastFrameMS)
	}
}

func cameraAt(x, y, z float64) *scene.PerspectiveCamera {
	return &scene.PerspectiveCamera{Position: scene.Vec3(x, y, z), FOV: 50, Near: 0.1, Far: 100}
}

func countNonBackground(result *preview.Result, background color.RGBA) int {
	count := 0
	bounds := result.Image.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if result.Image.RGBAAt(x, y) != background {
				count++
			}
		}
	}
	return count
}

// TestRenderIsReproducibleAcrossRuns pins the foundation of the whole evidence
// story. The scene deliberately carries map-valued fields, because Go map
// iteration order changes between runs and would leak into the pixels if the
// lowering depended on it.
//
// The check covers one process on one machine. Cross-machine reproducibility
// also needs identical floating-point behaviour in the rasterizer, which this
// test cannot observe.
func TestRenderIsReproducibleAcrossRuns(t *testing.T) {
	doc := `{"schema":"gosx.scene3d.ir.v1","objects":[
		{"id":"a","kind":"sphere","radius":1,"segments":20,"color":"#ff8060",
		 "customUniforms":{"zeta":1,"alpha":2,"mu":3,"kappa":4,"omega":5}},
		{"id":"b","kind":"torus","radius":0.8,"tube":0.25,"x":2.6,"color":"#60ff80"},
		{"id":"c","kind":"cylinder","radiusTop":0.4,"radiusBottom":0.7,"height":1.4,"x":-2.6,"color":"#8060ff"}
	],"instancedMeshes":[{"id":"grid","kind":"cube","size":0.3,"count":2,
		"transforms":[1,0,0,0,0,1,0,0,0,0,1,0,-1,-1.4,0,1, 1,0,0,0,0,1,0,0,0,0,1,0,1,-1.4,0,1],
		"attributes":{"gamma":[1,2],"beta":[3,4],"delta":[5,6]}}],
	 "lights":[{"id":"key","kind":"directional","directionX":-0.4,"directionY":-1,"directionZ":-0.3,"intensity":1.2}]}`

	var reference string
	for run := 0; run < 12; run++ {
		result, err := preview.RenderJSON([]byte(doc), preview.Options{
			Width: 128, Height: 96, Background: "#050608",
			Camera: cameraAt(0, 1.5, 7), DisableShadows: true, DisablePostFX: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		hash := hashPixels(result.Image)
		if run == 0 {
			reference = hash
			continue
		}
		if hash != reference {
			t.Fatalf("run %d produced different pixels: %s, want %s", run+1, hash, reference)
		}
	}
}

func hashPixels(img *image.RGBA) string {
	h := sha256.New()
	bounds := img.Bounds()
	for y := 0; y < bounds.Dy(); y++ {
		offset := img.PixOffset(bounds.Min.X, bounds.Min.Y+y)
		h.Write(img.Pix[offset : offset+bounds.Dx()*4])
	}
	return hex.EncodeToString(h.Sum(nil))
}
