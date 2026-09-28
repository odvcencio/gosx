package motion

import (
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateCurveGolden = flag.Bool("update-curve-golden", false, "rewrite testdata/curve_golden.json")

type curveGoldenCase struct {
	Name    string      `json:"name"`
	Smooth  bool        `json:"smooth"`
	Stops   [][]float64 `json:"stops"`   // [at, value]
	Samples [][]float64 `json:"samples"` // [x, want]
}

func curveGoldenCases() []curveGoldenCase {
	cases := []curveGoldenCase{
		{Name: "linear", Smooth: false, Stops: [][]float64{{0, 0}, {0.5, 10}, {1, 4}}},
		{Name: "smooth-uniform", Smooth: true, Stops: [][]float64{{0, 0}, {0.25, 3}, {0.5, 10}, {0.75, 6}, {1, 4}}},
		{Name: "smooth-nonuniform", Smooth: true, Stops: [][]float64{{-1, 2}, {0, 2}, {0.1, 8}, {2, -3}}},
	}
	for i := range cases {
		lo, hi := cases[i].Stops[0][0], cases[i].Stops[len(cases[i].Stops)-1][0]
		for k := -2; k <= 22; k++ {
			x := lo + (hi-lo)*float64(k)/20
			cases[i].Samples = append(cases[i].Samples, []float64{x, 0})
		}
	}
	return cases
}

func curveStops(raw [][]float64) []CurveStop {
	out := make([]CurveStop, len(raw))
	for i, s := range raw {
		out[i] = CurveStop{At: s[0], Value: s[1]}
	}
	return out
}

func TestCurveGolden(t *testing.T) {
	path := filepath.Join("testdata", "curve_golden.json")
	cases := curveGoldenCases()
	for i := range cases {
		for j := range cases[i].Samples {
			cases[i].Samples[j][1] = CurveValue(curveStops(cases[i].Stops), cases[i].Smooth, cases[i].Samples[j][0])
		}
	}
	if *updateCurveGolden {
		data, err := json.MarshalIndent(cases, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want []curveGoldenCase
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) != len(cases) {
		t.Fatalf("golden has %d cases, want %d; rerun with -update-curve-golden", len(want), len(cases))
	}
	for i := range cases {
		for j := range cases[i].Samples {
			if math.Abs(want[i].Samples[j][1]-cases[i].Samples[j][1]) > 1e-12 || want[i].Samples[j][0] != cases[i].Samples[j][0] {
				t.Fatalf("%s sample %d: golden %v, computed %v", cases[i].Name, j, want[i].Samples[j], cases[i].Samples[j])
			}
		}
	}
}

func TestCurveValueProperties(t *testing.T) {
	stops := []CurveStop{{0, 1}, {1, 5}, {3, 2}}
	for _, smooth := range []bool{false, true} {
		for _, s := range stops {
			if got := CurveValue(stops, smooth, s.At); math.Abs(got-s.Value) > 1e-12 {
				t.Fatalf("smooth=%v: curve at stop %v = %v", smooth, s.At, got)
			}
		}
		if CurveValue(stops, smooth, -5) != 1 || CurveValue(stops, smooth, 9) != 2 {
			t.Fatalf("smooth=%v: curve must hold end values outside the stops", smooth)
		}
	}
	if got := CurveValue(stops, false, 0.5); got != 3 {
		t.Fatalf("linear midpoint = %v, want 3", got)
	}
	if CurveValue(nil, true, 1) != 0 || CurveValue([]CurveStop{{0, 7}}, true, 1) != 7 {
		t.Fatal("empty and single-stop curves misbehave")
	}
}

func TestCurveSignalValidation(t *testing.T) {
	p := NewProgram("bad")
	in := p.Time("t")
	p.Curve("one-stop", in, []CurveStop{{0, 1}}, false)
	if _, err := p.Marshal(); err == nil || !strings.Contains(err.Error(), "at least two") {
		t.Fatalf("err = %v, want two-stop error", err)
	}
	p = NewProgram("bad2")
	in = p.Time("t")
	p.Curve("unsorted", in, []CurveStop{{0, 1}, {0, 2}}, false)
	if _, err := p.Marshal(); err == nil || !strings.Contains(err.Error(), "strictly increase") {
		t.Fatalf("err = %v, want ordering error", err)
	}
	p = NewProgram("bad3")
	p.Curve("no-input", "", []CurveStop{{0, 1}, {1, 2}}, false)
	if _, err := p.Marshal(); err == nil || !strings.Contains(err.Error(), "input signal") {
		t.Fatalf("err = %v, want input error", err)
	}
	p = NewProgram("nan")
	in = p.Time("t")
	p.Curve("nan", in, []CurveStop{{0, 1}, {1, math.NaN()}}, false)
	if _, err := p.Marshal(); err == nil {
		t.Fatal("NaN stop must be rejected")
	}
}

func TestCameraRailBuildsBindingsAndLooksAtTarget(t *testing.T) {
	p := NewProgram("rail")
	scroll := p.ScrollProgress("scroll", "#stage", AxisY)
	stops := []RailStop{
		{At: 0, Position: [3]float64{0, 1, 8}, LookAt: [3]float64{0, 0, 0}, FOV: 50},
		{At: 0.5, Position: [3]float64{6, 2, 4}, LookAt: [3]float64{0, 0, 0}, FOV: 40},
		{At: 1, Position: [3]float64{0, 6, 0.5}, LookAt: [3]float64{0, 0, 0}, FOV: 60},
	}
	if err := p.CameraRail("rail", scroll, "#scene", stops); err != nil {
		t.Fatal(err)
	}
	data, err := p.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Program
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	props := map[string]bool{}
	for _, b := range got.Bindings {
		if b.Target != BindingCamera || b.Selector != "#scene" {
			t.Fatalf("unexpected binding %#v", b)
		}
		props[b.Property] = true
	}
	for _, want := range []string{"position.x", "position.y", "position.z", "rotation.x", "rotation.y", "fov"} {
		if !props[want] {
			t.Fatalf("missing camera binding %s; have %v", want, props)
		}
	}

	// At every sampled progress the camera forward vector must point at the
	// origin (the look-at target) within a small angular error.
	series := map[string][]CurveStop{}
	for _, s := range p.Signals {
		if s.Kind != SignalCurve {
			continue
		}
		var cs []CurveStop
		for _, f := range s.Frames {
			cs = append(cs, CurveStop{At: f.At, Value: f.Value.(float64)})
		}
		series[s.ID] = cs
	}
	for i := 0; i <= 40; i++ {
		at := float64(i) / 40
		pos := [3]float64{
			CurveValue(series["rail.x"], true, at),
			CurveValue(series["rail.y"], true, at),
			CurveValue(series["rail.z"], true, at),
		}
		rx := CurveValue(series["rail.rotationX"], false, at)
		ry := CurveValue(series["rail.rotationY"], false, at)
		forward := [3]float64{-math.Cos(rx) * math.Sin(ry), math.Sin(rx), -math.Cos(rx) * math.Cos(ry)}
		toTarget := [3]float64{-pos[0], -pos[1], -pos[2]}
		length := math.Sqrt(toTarget[0]*toTarget[0] + toTarget[1]*toTarget[1] + toTarget[2]*toTarget[2])
		dot := (forward[0]*toTarget[0] + forward[1]*toTarget[1] + forward[2]*toTarget[2]) / length
		if dot < math.Cos(2*math.Pi/180) {
			t.Fatalf("at progress %.3f the camera is %.2f degrees off the look-at target", at, math.Acos(math.Min(1, dot))*180/math.Pi)
		}
	}
}

func TestCameraRailValidation(t *testing.T) {
	stop := func(at float64) RailStop { return RailStop{At: at, Position: [3]float64{0, 0, 5}} }
	cases := map[string][]RailStop{
		"one stop":    {stop(0)},
		"unordered":   {stop(1), stop(0)},
		"partial FOV": {{At: 0, FOV: 50}, {At: 1}},
		"FOV range":   {{At: 0, FOV: 200}, {At: 1, FOV: 40}},
		"non-finite":  {{At: 0, Position: [3]float64{math.Inf(1), 0, 0}}, stop(1)},
	}
	for name, stops := range cases {
		p := NewProgram("r")
		if err := p.CameraRail("r", p.Time("t"), "#scene", stops); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
	p := NewProgram("r")
	if err := p.CameraRail("r", p.Time("t"), " ", []RailStop{stop(0), stop(1)}); err == nil {
		t.Fatal("blank scene selector must be rejected")
	}
}

func TestCameraRailYawStaysContinuousAcrossTheSeam(t *testing.T) {
	// Looking from +z toward -z is yaw 0; sweeping the position around the
	// target crosses the +-pi seam, which must not produce a 2*pi jump.
	p := NewProgram("seam")
	stops := []RailStop{
		{At: 0, Position: [3]float64{0, 0, -5}, LookAt: [3]float64{0, 0, 0}},
		{At: 1, Position: [3]float64{-1, 0, -5}, LookAt: [3]float64{0, 0, 0}},
		{At: 2, Position: [3]float64{-5, 0, -1}, LookAt: [3]float64{0, 0, 0}},
	}
	if err := p.CameraRail("seam", p.Time("t"), "#scene", stops); err != nil {
		t.Fatal(err)
	}
	for _, s := range p.Signals {
		if s.ID != "seam.rotationY" {
			continue
		}
		for i := 1; i < len(s.Frames); i++ {
			d := math.Abs(s.Frames[i].Value.(float64) - s.Frames[i-1].Value.(float64))
			if d > math.Pi/2 {
				t.Fatalf("yaw jumps by %v between samples %d and %d", d, i-1, i)
			}
		}
	}
}

// A smooth curve must stay within the value range of the two stops that bound
// each segment. This is the property camera rails rely on to keep FOV valid.
func TestSmoothCurveNeverOvershootsItsStops(t *testing.T) {
	cases := [][]CurveStop{
		{{0, 1}, {1, 1}, {2, 179}},
		{{0, 1}, {1, 179}, {2, 1}, {3, 60}},
		{{0, 5}, {0.1, 5.5}, {5, -3}, {5.2, -3.1}, {9, 40}},
		{{0, 0}, {1, 10}},
	}
	for ci, stops := range cases {
		for i := 0; i < len(stops)-1; i++ {
			lo := math.Min(stops[i].Value, stops[i+1].Value)
			hi := math.Max(stops[i].Value, stops[i+1].Value)
			for k := 0; k <= 200; k++ {
				x := stops[i].At + (stops[i+1].At-stops[i].At)*float64(k)/200
				if v := CurveValue(stops, true, x); v < lo-1e-9 || v > hi+1e-9 {
					t.Fatalf("case %d segment %d: curve at %v = %v, outside [%v, %v]", ci, i, x, v, lo, hi)
				}
			}
		}
	}
}

func TestCameraRailFOVStaysWithinStopRange(t *testing.T) {
	p := NewProgram("fov")
	stops := []RailStop{
		{At: 0, Position: [3]float64{0, 0, 5}, FOV: 1},
		{At: 1, Position: [3]float64{1, 0, 5}, FOV: 1},
		{At: 2, Position: [3]float64{2, 0, 5}, FOV: 179},
	}
	if err := p.CameraRail("r", p.Time("t"), "#scene", stops); err != nil {
		t.Fatal(err)
	}
	for _, s := range p.Signals {
		if s.ID != "r.fov" {
			continue
		}
		var cs []CurveStop
		for _, f := range s.Frames {
			cs = append(cs, CurveStop{At: f.At, Value: f.Value.(float64)})
		}
		for k := 0; k <= 400; k++ {
			if v := CurveValue(cs, s.Smooth, 2*float64(k)/400); v < 1-1e-9 || v > 179+1e-9 {
				t.Fatalf("FOV %v at %d is outside 1 to 179", v, k)
			}
		}
	}
}
