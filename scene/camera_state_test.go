package scene

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestCameraCommandNormalizesTypedStateAndKeepsSparsePatches(t *testing.T) {
	for _, camera := range []any{PerspectiveCamera{Position: Vec3(0, 3, 0), FOV: 40}, IRCamera{Kind: "perspective", Y: 3, FOV: 40}, OrthographicCamera{Position: Vec3(0, 3, 0), Left: -2, Right: 2}} {
		data, err := json.Marshal(SetCameraCommand(camera))
		if err != nil {
			t.Fatal(err)
		}
		var command struct{ Data map[string]any }
		if err = json.Unmarshal(data, &command); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"x", "z", "rotationX", "rotationY", "rotationZ", "portraitFOV"} {
			if command.Data[field] != float64(0) {
				t.Fatalf("%s zero reset absent: %s", field, data)
			}
		}
		if _, ok := command.Data["Position"]; ok {
			t.Fatal("capitalized typed camera leaked")
		}
	}
	sparse := map[string]any{"fov": 50.0}
	command := SetCameraCommand(sparse)
	if len(command.Data.(map[string]any)) != 1 {
		t.Fatal("timeline patch replaced complete state")
	}
}

func TestCameraTransitionInterruptionEndpointsAndOrthographic(t *testing.T) {
	from := IRCamera{Kind: "perspective", X: 2, Y: 3, Z: 8, RotationX: -.5, FOV: 36}
	to := IRCamera{Kind: "perspective", Y: 12, Z: 2, RotationX: -1.4, FOV: 44, TransitionMS: 400}
	move := CameraTransition{From: from, To: to, Duration: 400 * time.Millisecond, FOVArc: 8}
	readback := from
	readback.Zoom = 1
	if !SameCameraPose(from, readback, .0001) {
		t.Fatal("inactive orthographic defaults restarted a perspective move")
	}
	start, running := move.Sample(0)
	if !running || start != from {
		t.Fatal("start jumped", start)
	}
	mid, running := move.Sample(200 * time.Millisecond)
	if !running || mid.Y <= from.Y || mid.Y >= to.Y {
		t.Fatal("missing intermediate pose")
	}
	reverse := CameraTransition{From: mid, To: from, Duration: 400 * time.Millisecond}
	resumed, _ := reverse.Sample(0)
	if resumed != mid {
		t.Fatal("interrupted camera snapped")
	}
	final, running := move.Sample(move.Duration)
	to.TransitionMS = 0
	if running || final != to {
		t.Fatal("endpoint not exact", final)
	}
	move.Duration = 0
	if got, r := move.Sample(0); r || got != to {
		t.Fatal("reduced motion did not settle")
	}
	move = CameraTransition{From: IRCamera{Kind: "orthographic", Left: -1, Right: 1, Zoom: 1}, To: IRCamera{Kind: "orthographic", Left: -4, Right: 4, Zoom: 2}, Duration: time.Second}
	mid, _ = move.Sample(time.Second / 2)
	if mid.Left >= -1 || mid.Left <= -4 || mid.Zoom <= 1 || mid.Zoom >= 2 {
		t.Fatal("orthographic lens did not interpolate")
	}
}

func TestPerspectiveFitAndNativeCameraRay(t *testing.T) {
	points := []Vector3{Vec3(-3, .2, -2), Vec3(4, .4, 2), Vec3(1, 2, 0)}
	for _, aspect := range []float64{.5, 1.6, 3} {
		opts := PerspectiveFit{Aspect: aspect, Pitch: .8, FOV: 40, Left: .08, Right: .95, Top: .17, Bottom: .84, Near: .1, Far: 300, MinDepth: .5}
		c, err := FitPerspectivePoints(points, opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range points {
			x, y, z := p.X-c.Position.X, p.Y-c.Position.Y, p.Z-c.Position.Z
			y, z = y*math.Cos(c.Rotation.X)-z*math.Sin(c.Rotation.X), y*math.Sin(c.Rotation.X)+z*math.Cos(c.Rotation.X)
			tangent := math.Tan(c.FOV * math.Pi / 360)
			sx, sy := .5+x/(-2*z*tangent*aspect), .5-y/(-2*z*tangent)
			if sx < opts.Left-1e-10 || sx > opts.Right+1e-10 || sy < opts.Top-1e-10 || sy > opts.Bottom+1e-10 {
				t.Fatal("point escaped fit", sx, sy)
			}
		}
	}
	ray, err := CameraRay(IRCamera{Kind: "perspective", X: 2, Y: 3, Z: 8, FOV: 40}, 50, 50, 100, 100)
	if err != nil || ray.Origin != Vec3(2, 3, 8) || ray.Direction != Vec3(0, 0, -1) {
		t.Fatal("native world convention", ray, err)
	}
	if _, err = CameraRay(IRCamera{Kind: "perspective", FOV: 40}, -1, 0, 100, 100); err == nil {
		t.Fatal("outside ray accepted")
	}
	if _, err = FitPerspectivePoints(points, PerspectiveFit{}); err == nil {
		t.Fatal("invalid fit accepted")
	}
}
