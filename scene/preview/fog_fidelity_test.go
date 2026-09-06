package preview_test

import (
	"bytes"
	"errors"
	"io"
	"math"
	"reflect"
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/preview"
)

func TestFogLitAttachmentsAndOrthographic(t *testing.T) {
	for _, ortho := range []bool{false, true} {
		p := scene.Props{Background: "#000000", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 5)}, Graph: scene.NewGraph(scene.Mesh{ID: "fog-box", Pickable: scene.Bool(true), Geometry: scene.BoxGeometry{Width: 2, Height: 2, Depth: 1}, Material: scene.StandardMaterial{Color: "#ff0000", Roughness: 1}})}
		if ortho {
			p.OrthographicCamera = &scene.OrthographicCamera{Position: scene.Vec3(0, 0, 5), Left: -3, Right: 3, Top: 3, Bottom: -3, Zoom: 1}
		}
		base, hit, depth := affineReadback(t, p)
		p.Environment.FogColor, p.Environment.FogDensity = "#ffffff", .6
		fog, fogHit, fogDepth := affineReadback(t, p)
		if hit.ObjectID != "fog-box" || !reflect.DeepEqual(hit, fogHit) || depth != fogDepth {
			t.Fatalf("fog changed pick/depth: %v %v %g %g", hit, fogHit, depth, fogDepth)
		}
		before, after := base.RGBAAt(32, 32), fog.RGBAAt(32, 32)
		if after.G <= before.G || after.B <= before.B {
			t.Fatalf("fog did not move toward white: %v -> %v", before, after)
		}
		for i := 3; i < len(base.Pix); i += 4 {
			if base.Pix[i] != fog.Pix[i] {
				t.Fatal("fog changed alpha")
			}
		}
	}
}

func TestFogFidelityGate(t *testing.T) {
	p := scene.Props{Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 5)}, Graph: scene.NewGraph(scene.Mesh{Geometry: scene.BoxGeometry{Width: 2, Height: 1, Depth: 1}})}
	opts := preview.Options{Width: 16, Height: 16, DisableShadows: true, DisablePostFX: true}
	base, err := preview.Render(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		density float64
		color   string
	}{{0, "#ffffff"}, {-.2, "#ffffff"}, {.6, "#ffffff"}, {.3, ""}, {math.NaN(), "#ffffff"}, {math.Inf(1), "#ffffff"}} {
		density := tc.density
		p.Environment.FogColor = tc.color
		p.Environment.FogDensity = density
		active := density > 0 && !math.IsNaN(density) && !math.IsInf(density, 0)
		for _, wire := range []bool{false, true} {
			if wire && (math.IsNaN(density) || math.IsInf(density, 0)) {
				continue
			} // JSON cannot encode nonfinite numbers.
			var result *preview.Result
			if wire {
				result, err = preview.RenderJSON(p.RawPropsJSON(), opts)
			} else {
				result, err = preview.Render(p, opts)
			}
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(base.Image.Pix, result.Image.Pix) == active {
				t.Fatalf("lit fog pixel activity mismatch: active=%v", active)
			}
			count, info := 0, 0
			for _, d := range result.Bundle.Diagnostics {
				if d.Code == "scene.preview.unsupported_environment_fog" {
					count++
				}
				if d.Code == "scene.preview.environment_fields_ignored" {
					info++
				}
			}
			if (active && (count != 1 || info != 0)) || (!active && (count != 0 || info != 1)) {
				t.Fatalf("density=%g wire=%v count=%d info=%d", density, wire, count, info)
			}
		}
	}
	p.Environment.FogDensity = .6
	po := preview.NewPosterOptions(opts)
	po.Floors.UniqueColors = 2 // isolate capability omission from this simple fixture's shading floor.
	for _, allow := range []bool{false, true} {
		po.Floors.AllowDroppedRecords = allow
		poster, err := preview.PosterFromJSON(p.RawPropsJSON(), po)
		if err != nil {
			t.Fatal(err)
		}
		if len(poster.Fidelity.Dropped) != 1 || poster.Fidelity.Dropped[0].Code != "scene.preview.unsupported_environment_fog" {
			t.Fatal(poster.Fidelity.Dropped)
		}
		err = preview.WritePoster(io.Discard, poster)
		if (!allow && !errors.Is(err, preview.ErrLowFidelity)) || (allow && err != nil) {
			t.Fatalf("allow=%v: %v", allow, err)
		}
	}
}
