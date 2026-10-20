package preview_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene/preview"
)

func writeTexture(t *testing.T, path string, c color.RGBA) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func diagnosticsByCode(result *preview.Result, code string) []engine.RenderDiagnostic {
	var out []engine.RenderDiagnostic
	for _, diagnostic := range result.Bundle.Diagnostics {
		if diagnostic.Code == code {
			out = append(out, diagnostic)
		}
	}
	return out
}

const texturedScene = `{"schema":"gosx.scene3d.ir.v1","objects":[
	{"id":"panel","kind":"cube","size":3,"texture":"/textures/skin.png","color":"#ffffff"}
],"lights":[{"id":"sun","kind":"directional","directionX":-0.3,"directionY":-0.6,"directionZ":-1,"intensity":1.6}]}`

// TestAssetRootsMakeTexturesVisible proves the preview draws a real texture
// once a root supplies it, instead of the rasterizer's placeholder checker.
func TestAssetRootsMakeTexturesVisible(t *testing.T) {
	root := t.TempDir()
	writeTexture(t, filepath.Join(root, "textures", "skin.png"), color.RGBA{R: 20, G: 220, B: 90, A: 255})

	options := preview.Options{Width: 96, Height: 64, Background: "#000000",
		Camera: cameraAt(0, 0, 6), DisableShadows: true, DisablePostFX: true}

	withoutRoot, err := preview.RenderJSON([]byte(texturedScene), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnosticsByCode(withoutRoot, "scene.preview.unresolved_texture")) != 0 {
		t.Fatal("without an asset root the preview must not claim it searched for the texture")
	}

	options.AssetRoots = []string{root}
	withRoot, err := preview.RenderJSON([]byte(texturedScene), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnosticsByCode(withRoot, "scene.preview.unresolved_texture")) != 0 {
		t.Fatalf("a resolvable texture must not be reported unresolved: %+v", withRoot.Bundle.Diagnostics)
	}
	if withRoot.Bundle.Materials[0].Texture == "/textures/skin.png" {
		t.Fatal("the texture source was not rewritten onto a real file")
	}
	if hashPixels(withRoot.Image) == hashPixels(withoutRoot.Image) {
		t.Fatal("resolving the texture must change the frame")
	}
	// The resolved texture is a flat green, so the lit plane must read green.
	if !hasGreenDominantPixel(withRoot.Image) {
		t.Fatal("the resolved texture did not reach the pixels")
	}
}

// TestUnresolvedTextureIsReported proves a texture that no root supplies gets
// named, instead of quietly becoming a placeholder checker.
func TestUnresolvedTextureIsReported(t *testing.T) {
	result, err := preview.RenderJSON([]byte(texturedScene), preview.Options{
		Width: 64, Height: 48, AssetRoots: []string{t.TempDir()},
		DisableShadows: true, DisablePostFX: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := diagnosticsByCode(result, "scene.preview.unresolved_texture")
	if len(diagnostics) != 1 {
		t.Fatalf("expected one unresolved-texture diagnostic: %+v", result.Bundle.Diagnostics)
	}
	if diagnostics[0].Target != "/textures/skin.png" {
		t.Fatalf("diagnostic must name the source: %+v", diagnostics[0])
	}
	if !strings.Contains(diagnostics[0].Message, "placeholder checker") {
		t.Fatalf("diagnostic must state what the frame shows instead: %s", diagnostics[0].Message)
	}
}

// TestIgnoredMaterialFieldsAreReported proves the preview names the material
// fields the CPU path still cannot express, and names ONLY those.
//
// It used to assert roughness, metalness and normalMap were reported as
// ignored. That was true against a Lambert term and is now false: the rasterizer
// runs the whole litWGSL fragment stage and shades all three. Reporting them
// would tell an author their material does nothing while it is shading, which
// invites deleting work that functions.
func TestIgnoredMaterialFieldsAreReported(t *testing.T) {
	doc := `{"schema":"gosx.scene3d.ir.v1","objects":[
		{"id":"a","kind":"sphere","radius":1,"roughness":0.9,"metalness":0.4,"normalMap":"/n.png","materialKind":"glass"},
		{"id":"b","kind":"cube","size":1,"x":3,"roughness":0.2,"wireframe":true}
	]}`
	result, err := preview.RenderJSON([]byte(doc), preview.Options{Width: 64, Height: 48, DisableShadows: true, DisablePostFX: true})
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := diagnosticsByCode(result, "scene.preview.material_fields_ignored")
	if len(diagnostics) != 1 {
		t.Fatalf("expected one material coverage note: %+v", result.Bundle.Diagnostics)
	}
	message := diagnostics[0].Message
	for _, want := range []string{"wireframe(1)", "materialKind(1)"} {
		if !strings.Contains(message, want) {
			t.Fatalf("material note missing %q: %s", want, message)
		}
	}
	// The other half, and the one that matters more. A field the fragment stage
	// shades must NOT appear here. Naming a working field is the expensive
	// direction of a wrong diagnostic.
	for _, shaded := range []string{"roughness", "metalness", "normalMap", "clearcoat", "sheen"} {
		if strings.Contains(message, shaded) {
			t.Fatalf("%q reaches a CPU pixel and must not be reported as ignored: %s", shaded, message)
		}
	}
	if diagnostics[0].Severity != "info" {
		t.Fatalf("the material note must not read as a failure: %s", diagnostics[0].Severity)
	}
}

// TestNoMaterialNoteWhenNothingIsIgnored keeps the note out of the way of
// scenes that only use fields the rasterizer reads.
func TestNoMaterialNoteWhenNothingIsIgnored(t *testing.T) {
	doc := `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"a","kind":"cube","size":2,"color":"#ff8060","opacity":0.8}]}`
	result, err := preview.RenderJSON([]byte(doc), preview.Options{Width: 48, Height: 32, DisableShadows: true, DisablePostFX: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnosticsByCode(result, "scene.preview.material_fields_ignored")) != 0 {
		t.Fatalf("unexpected material note: %+v", result.Bundle.Diagnostics)
	}
}

// TestMaterialNoteCountsEnabledAlphaCutoffOnly proves the alphaCutoff count is
// honest: only a numeric cutoff promises a pixel effect the CPU rasterizer
// does not produce, so only it is counted; an explicit null disable and an
// absent cutoff must not raise the note.
func TestMaterialNoteCountsEnabledAlphaCutoffOnly(t *testing.T) {
	doc := `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"a","kind":"cube","size":2,"color":"#ff8060","alphaCutoff":0.5}]}`
	result, err := preview.RenderJSON([]byte(doc), preview.Options{Width: 48, Height: 32, DisableShadows: true, DisablePostFX: true})
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := diagnosticsByCode(result, "scene.preview.material_fields_ignored")
	if len(diagnostics) != 1 {
		t.Fatalf("expected one material coverage note for enabled alphaCutoff: %+v", result.Bundle.Diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, "alphaCutoff(1)") {
		t.Fatalf("material note missing alphaCutoff(1): %s", diagnostics[0].Message)
	}

	disabled := `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"a","kind":"cube","size":2,"color":"#ff8060","alphaCutoff":null}]}`
	result, err = preview.RenderJSON([]byte(disabled), preview.Options{Width: 48, Height: 32, DisableShadows: true, DisablePostFX: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := diagnosticsByCode(result, "scene.preview.material_fields_ignored"); len(got) != 0 {
		t.Fatalf("explicit null alphaCutoff must not raise the note: %+v", got)
	}

	// A numeric zero still promises a pixel effect, so it counts.
	zero := `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"a","kind":"cube","size":2,"color":"#ff8060","alphaCutoff":0}]}`
	result, err = preview.RenderJSON([]byte(zero), preview.Options{Width: 48, Height: 32, DisableShadows: true, DisablePostFX: true})
	if err != nil {
		t.Fatal(err)
	}
	diagnostics = diagnosticsByCode(result, "scene.preview.material_fields_ignored")
	if len(diagnostics) != 1 {
		t.Fatalf("expected one material coverage note for numeric zero alphaCutoff: %+v", result.Bundle.Diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, "alphaCutoff(1)") {
		t.Fatalf("material note missing alphaCutoff(1) for zero cutoff: %s", diagnostics[0].Message)
	}

	// An omitted cutoff promises nothing and must not raise the note.
	omitted := `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"a","kind":"cube","size":2,"color":"#ff8060"}]}`
	result, err = preview.RenderJSON([]byte(omitted), preview.Options{Width: 48, Height: 32, DisableShadows: true, DisablePostFX: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := diagnosticsByCode(result, "scene.preview.material_fields_ignored"); len(got) != 0 {
		t.Fatalf("omitted alphaCutoff must not raise the note: %+v", got)
	}

	// A numeric cutoff on an instanced mesh record counts too.
	instanced := `{"schema":"gosx.scene3d.ir.v1","instancedMeshes":[{"id":"i","kind":"cube","count":1,"transforms":[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1],"alphaCutoff":0.5}]}`
	result, err = preview.RenderJSON([]byte(instanced), preview.Options{Width: 48, Height: 32, DisableShadows: true, DisablePostFX: true})
	if err != nil {
		t.Fatal(err)
	}
	diagnostics = diagnosticsByCode(result, "scene.preview.material_fields_ignored")
	if len(diagnostics) != 1 {
		t.Fatalf("expected one material coverage note for instanced alphaCutoff: %+v", result.Bundle.Diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, "alphaCutoff(1)") {
		t.Fatalf("material note missing alphaCutoff(1) for instanced cutoff: %s", diagnostics[0].Message)
	}
}

func greenRGBA() color.RGBA { return color.RGBA{R: 20, G: 220, B: 90, A: 255} }

func hasGreenDominantPixel(img *image.RGBA) bool {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if c.G > 60 && int(c.G) > int(c.R)+40 && int(c.G) > int(c.B)+40 {
				return true
			}
		}
	}
	return false
}
