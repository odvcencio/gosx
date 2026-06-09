// Package ortho2dspike is a THROWAWAY M1 de-risk spike for the gosx-studio
// WebGPU canvas re-platform (plan.gosx-studio.m1-16a-ortho2d-board.v0.1).
//
// FINDING (2026-06-09): the canvas board has NO GPU render path today. bundle2d
// emits the board's rects into RenderBundle's 2D-painter display list
// ("objects": {kind,vertexCount,bounds,materialIndex}) — the format the
// 2D-context painter 26b1-canvas2d-painter.js consumes. The GPU scene renderer
// (render/bundle) and 16a draw InstancedMeshes / Surfaces / Lines, NOT "objects",
// so they render the canvas2d bundle as BACKGROUND ONLY. OrthoCamera2D +
// computeOrthoCamera2DMVP exist in render/bundle but nothing feeds them
// GPU-drawable board geometry.
//
// Consequence: M1 slice 1 is NOT "feed bundle2d's bundle to 16a". It must first
// give the board a GPU-geometry representation — emit each rect as an
// InstancedMesh/Surface quad (which drawInstancedMeshes/drawSurfaceEntries
// render), or add an "objects" 2D-quad draw path to the renderer. The geometry
// BUDGET cannot be measured until that path exists (a quad pipeline is trivially
// cheap on-GPU, but it has to actually draw first).
//
// This test PASSES by asserting the gap, so the finding is a durable regression.
// DELETE / supersede once M1 slice 1 lands the GPU board path.
package ortho2dspike

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/render/bundle2d"
	"m31labs.dev/gosx/render/gpu/headless"
)

// TestOrtho2DObjectQuadRenders is the M1 slice-1 de-risk: prove the EXISTING
// native object renderer (drawObjectMeshes) draws a board quad when the board's
// Objects reference real WorldPositions — i.e. the DRY fix is "emit
// WorldPositions+normals for the board's existing objects", reusing the object
// draw path on both native + 16a (no new pipeline). One orange quad at board
// XY [-50,50]^2, OrthoCamera2D, Unlit material.
func TestOrtho2DObjectQuadRenders(t *testing.T) {
	if os.Getenv("GOSX_ORTHO2D_BUDGET") == "" {
		t.Skip("throwaway M1 spike; set GOSX_ORTHO2D_BUDGET=1 to run (hits the GPU)")
	}
	const w, h = 200, 200
	quad := []float64{
		-50, -50, 0, 50, -50, 0, 50, 50, 0, // tri 1
		-50, -50, 0, 50, 50, 0, -50, 50, 0, // tri 2
	}
	nrm := make([]float64, 0, 18)
	uv := make([]float64, 0, 12)
	for i := 0; i < 6; i++ {
		nrm = append(nrm, 0, 0, 1)
		uv = append(uv, 0, 0)
	}
	rb := rootengine.RenderBundle{
		Background:     "#101018",
		Camera:         bundle.OrthoCamera2D(1, 0, 0, w, h),
		Materials:      []rootengine.RenderMaterial{{Kind: "flat", Color: "#ff8800", Unlit: true}},
		Objects:        []rootengine.RenderObject{{ID: "q", Kind: "rect", VertexOffset: 0, VertexCount: 6, MaterialIndex: 0}},
		WorldPositions: quad,
		WorldNormals:   nrm,
		WorldUVs:       uv,
	}

	d, surface := headless.New(w, h)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	if err := r.Frame(rb, w, h, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	img := d.Framebuffer()
	r.Destroy()

	ctr := img.RGBAAt(w/2, h/2)
	cor := img.RGBAAt(8, 8)
	t.Logf("center(100,100)=R%d G%d B%d  corner(8,8)=R%d G%d B%d", ctr.R, ctr.G, ctr.B, cor.R, cor.G, cor.B)
	// PROVEN: the object path draws the board quad under ortho-2D — center is the
	// orange material (R>G>B hue, far brighter than the background corner).
	if !(ctr.R > 60 && ctr.R > cor.R+40 && ctr.R > ctr.G && ctr.G >= ctr.B) {
		t.Errorf("board quad did not render via the object path: center R%d G%d B%d vs corner R%d G%d B%d", ctr.R, ctr.G, ctr.B, cor.R, cor.G, cor.B)
	}
	if cor.R > 80 || cor.G > 80 {
		t.Errorf("corner is not background: R%d G%d B%d", cor.R, cor.G, cor.B)
	}
	// FOLLOW-UP (not blocking): #ff8800 rendered ~1/3 brightness (R85 not 255) —
	// drawObjectMeshes uses the lit pipeline and applies ambient even for Unlit
	// materials. The 2D/ortho board path must output the flat color at full
	// brightness (honored in the 16a port; RenderMaterial.CustomFragmentWGSL is
	// the Selena hook for the fill shader).
	if ctr.R < 200 {
		t.Logf("FOLLOW-UP: flat color dimmed to R%d (expected ~255) — unlit-in-2D not yet honored", ctr.R)
	}
}

// TestCanvas2DBundleHasNoGPUGeometry_M1Gap documents the M1 gap: the bundle2d
// canvas board carries its geometry in "objects" (the 26b1 painter format) with
// the GPU geometry fields empty, so the WebGPU scene renderer draws nothing but
// the cleared background.
func TestCanvas2DBundleHasNoGPUGeometry_M1Gap(t *testing.T) {
	if os.Getenv("GOSX_ORTHO2D_BUDGET") == "" {
		t.Skip("throwaway M1 spike; set GOSX_ORTHO2D_BUDGET=1 to run (hits the GPU)")
	}
	const w, h = 360, 180
	nodes := []gosx.CanvasBoardNode{
		{ID: "r", Kind: "rect", X: -100, Y: 0, Width: 70, Height: 70, Color: "#ff0000"},
		{ID: "g", Kind: "rect", X: 0, Y: 0, Width: 70, Height: 70, Color: "#00ff00"},
		{ID: "b", Kind: "rect", X: 100, Y: 0, Width: 70, Height: 70, Color: "#0000ff"},
	}
	rb := bundle2d.ComputeCanvasBundleWithBackground(nodes, "#101018", w, h, 1, 0, 0)

	// 1. The rects are in the 2D-painter "objects" display list, not GPU fields.
	js, err := bundle2d.MarshalCanvasBundle(rb)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(js, `"objects"`) || !strings.Contains(js, `"kind":"rect"`) {
		t.Fatalf("expected rects in the 2D-painter \"objects\" list; got: %s", js)
	}
	if len(rb.InstancedMeshes) != 0 || len(rb.Surfaces) != 0 || len(rb.Lines) != 0 {
		t.Fatalf("expected EMPTY GPU geometry fields (the gap); got InstancedMeshes=%d Surfaces=%d Lines=%d",
			len(rb.InstancedMeshes), len(rb.Surfaces), len(rb.Lines))
	}

	// 2. Therefore the GPU scene renderer paints background only.
	d, surface := headless.New(w, h)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	if err := r.Frame(rb, w, h, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	img := d.Framebuffer()
	r.Destroy()

	colored := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := img.RGBAAt(x, y)
			// anything clearly not the #101018 background
			if c.R > 80 || c.G > 80 || c.B > 90 {
				colored++
			}
		}
	}
	if colored != 0 {
		t.Errorf("unexpected: %d non-background pixels — the GPU path may now draw the board (revisit M1 scope)", colored)
	}
	t.Logf("CONFIRMED M1 gap: canvas2d bundle renders %d/%d non-bg pixels via the GPU renderer (objects ignored; InstancedMeshes/Surfaces empty). M1 must emit GPU board geometry.", colored, w*h)
}
