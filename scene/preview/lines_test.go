package preview_test

import (
	"bytes"
	"errors"
	"io"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/preview"
	"math"
	"strings"
	"testing"
)

func TestNativeThinLineRawStyle(t *testing.T) {
	for _, tc := range []struct {
		name             string
		width, dash, gap float64
		admitted         bool
	}{
		{"default", 0, 0, 0, true},
		{"one", 1, 0, 0, true},
		{"thin", .5, 0, 0, true},
		{"negative-width", -1, 0, 0, false},
		{"nan-width", math.NaN(), 0, 0, false},
		{"positive-infinite-width", math.Inf(1), 0, 0, false},
		{"negative-infinite-width", math.Inf(-1), 0, 0, false},
		{"positive-dash", 0, 1, 0, false},
		{"negative-dash", 0, -1, 0, false},
		{"nan-dash", 0, math.NaN(), 0, false},
		{"infinite-dash", 0, math.Inf(1), 0, false},
		{"positive-gap", 0, 0, 1, false},
		{"negative-gap", 0, 0, -1, false},
		{"nan-gap", 0, 0, math.NaN(), false},
		{"infinite-gap", 0, 0, math.Inf(-1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := scene.ObjectIR{ID: tc.name, Kind: "lines", LineWidth: tc.width, DashSize: tc.dash, GapSize: tc.gap,
				Points: []scene.Vector3{scene.Vec3(-1, 0, 0), scene.Vec3(1, 0, 0)}, LineSegments: [][2]int{{0, 1}}}
			frame := preview.BundleIR(scene.SceneIR{Objects: []scene.ObjectIR{object}}, preview.Options{})
			if len(frame.InstancedMeshes) != 0 {
				t.Fatal("line entered primitive instancing")
			}
			warnings := 0
			for _, d := range frame.Diagnostics {
				if d.Code == "scene.preview.unsupported_geometry" {
					warnings++
					if !strings.Contains(d.Message, "finite width 0 (default) or 0<width<=1 and zero dash/gap metadata") {
						t.Fatal(d)
					}
				}
			}
			if tc.admitted {
				if len(frame.WorldPositions) != 6 || warnings != 0 {
					t.Fatalf("admitted line: positions=%d warnings=%d", len(frame.WorldPositions), warnings)
				}
			} else if len(frame.WorldPositions) != 0 || warnings != 1 {
				t.Fatalf("rejected line: positions=%d warnings=%d", len(frame.WorldPositions), warnings)
			}
		})
	}
}

func TestNativeThinLineIngress(t *testing.T) {
	p := scene.Props{Background: "#000000", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 5)}, Graph: scene.NewGraph(scene.Mesh{ID: "line", Geometry: scene.LinesGeometry{Points: []scene.Vector3{scene.Vec3(-1, 0, 0), scene.Vec3(1, 0, 0)}, Segments: [][2]int{{0, 1}}}, Material: scene.LineBasicMaterial{MaterialStyle: scene.MaterialStyle{Color: "#ff0000"}}})}
	r, err := preview.Render(p, preview.Options{Width: 32, Height: 32, DisablePostFX: true, DisableShadows: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Bundle.WorldPositions) != 6 || len(r.Bundle.InstancedMeshes) != 0 {
		t.Fatalf("line ingress missing: positions%d instances%d", len(r.Bundle.WorldPositions), len(r.Bundle.InstancedMeshes))
	}
	visible := 0
	for i := 0; i < len(r.Image.Pix); i += 4 {
		if r.Image.Pix[i] > 20 {
			visible++
		}
	}
	if visible == 0 {
		t.Fatal("line produced no pixels")
	}
}

func TestNativeThinLineOcclusionAndPick(t *testing.T) {
	box := scene.Mesh{ID: "box", Pickable: scene.Bool(true), Geometry: scene.BoxGeometry{Width: 2, Height: 2, Depth: 2}, Material: scene.StandardMaterial{Color: "#0000ff"}}
	p := scene.Props{Background: "#000000", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 5)}, Graph: scene.NewGraph(box)}
	base, hit, depth := affineReadback(t, p)
	if hit.ObjectID != "box" {
		t.Fatal(hit)
	}
	line := func(z float64) scene.Mesh {
		return scene.Mesh{ID: "line", Geometry: scene.LinesGeometry{Points: []scene.Vector3{scene.Vec3(-1, 0, z), scene.Vec3(1, 0, z)}, Segments: [][2]int{{0, 1}}}, Material: scene.LineBasicMaterial{MaterialStyle: scene.MaterialStyle{Color: "#ff0000", Opacity: scene.Float(.5)}}}
	}
	p.Graph = scene.NewGraph(box, line(-1))
	behind, bhit, bdepth := affineReadback(t, p)
	if !bytes.Equal(base.Pix, behind.Pix) || bhit.ObjectID != "box" || depth != bdepth {
		t.Fatal("behind line changed visible mesh/depth/pick")
	}
	p.Graph = scene.NewGraph(box, line(2))
	front, fhit, fdepth := affineReadback(t, p)
	if front.RGBAAt(32, 32).R <= base.RGBAAt(32, 32).R || fhit.ObjectID != "" || fdepth != depth {
		t.Fatalf("front color/pick/depth %v %+v %g/%g", front.RGBAAt(32, 32), fhit, fdepth, depth)
	}
}

func TestNativeThinLinePosterGate(t *testing.T) {
	p := scene.Props{Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 5)}, Graph: scene.NewGraph(scene.Mesh{ID: "wide", Geometry: scene.LinesGeometry{Width: 2, Points: []scene.Vector3{scene.Vec3(-1, 0, 0), scene.Vec3(1, 0, 0)}, Segments: [][2]int{{0, 1}}}})}
	opts := preview.NewPosterOptions(preview.Options{Width: 32, Height: 32, DisablePostFX: true, DisableShadows: true})
	poster, err := preview.PosterFromJSON(p.RawPropsJSON(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(poster.Fidelity.Dropped) == 0 {
		t.Fatal("wide line not dropped")
	}
	if err := preview.WritePoster(io.Discard, poster); !errors.Is(err, preview.ErrLowFidelity) {
		t.Fatalf("poster gate %v", err)
	}
}

func TestNativeThinLineControls(t *testing.T) {
	for _, ortho := range []bool{false, true} {
		p := scene.Props{Background: "#000000", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 5)}, Graph: scene.NewGraph(scene.Group{Position: scene.Vec3(.5, 0, 0), Scale: scene.Vec3(2, 1, 1), Children: []scene.Node{scene.Mesh{ID: "line", Geometry: scene.LinesGeometry{Points: []scene.Vector3{scene.Vec3(-1, 0, 0), scene.Vec3(1, 0, 0)}, Segments: [][2]int{{0, 1}}, Width: 1}, Material: scene.LineBasicMaterial{MaterialStyle: scene.MaterialStyle{Color: "#ff0000", Opacity: scene.Float(.5)}}}}})}
		if ortho {
			p.OrthographicCamera = &scene.OrthographicCamera{Position: scene.Vec3(0, 0, 5), Left: -3, Right: 3, Top: 3, Bottom: -3}
		}
		opts := preview.Options{Width: 32, Height: 32, DisablePostFX: true, DisableShadows: true}
		a, err := preview.Render(p, opts)
		if err != nil {
			t.Fatal(err)
		}
		b, err := preview.RenderJSON(p.RawPropsJSON(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a.Image.Pix, b.Image.Pix) || a.Bundle.WorldPositions[0] != -1.5 || a.Bundle.WorldPositions[3] != 2.5 || a.Bundle.WorldColors[3] != .5 {
			t.Fatal("transform/color/JSON drift")
		}
		for _, d := range a.Bundle.Diagnostics {
			if d.Code == "scene.preview.unsupported_geometry" {
				t.Fatal(d)
			}
		}
	}
	for _, tc := range []struct {
		name   string
		width  float64
		dash   bool
		points []scene.Vector3
		pairs  [][2]int
	}{
		{"thick", 2, false, []scene.Vector3{scene.Vec3(-1, 0, 0), scene.Vec3(1, 0, 0)}, [][2]int{{0, 1}}},
		{"dash", 1, true, []scene.Vector3{scene.Vec3(-1, 0, 0), scene.Vec3(1, 0, 0)}, [][2]int{{0, 1}}},
		{"empty", 0, false, nil, nil},
		{"index", 0, false, []scene.Vector3{scene.Vec3(0, 0, 0)}, [][2]int{{0, 2}}},
		{"nonfinite", 0, false, []scene.Vector3{scene.Vec3(math.NaN(), 0, 0), scene.Vec3(1, 0, 0)}, [][2]int{{0, 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var m scene.Material = scene.LineBasicMaterial{}
			if tc.dash {
				m = scene.LineDashedMaterial{}
			}
			p := scene.Props{Graph: scene.NewGraph(scene.Mesh{ID: tc.name, Geometry: scene.LinesGeometry{Points: tc.points, Segments: tc.pairs, Width: tc.width}, Material: m})}
			b := preview.Bundle(p, preview.Options{})
			if len(b.WorldPositions) != 0 || len(b.InstancedMeshes) != 0 {
				t.Fatal("invalid rendered")
			}
			found := false
			for _, d := range b.Diagnostics {
				if d.Code == "scene.preview.unsupported_geometry" {
					found = true
				}
			}
			if !found {
				t.Fatal("missing diagnostic")
			}
		})
	}
}
