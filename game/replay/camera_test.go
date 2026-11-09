package replay_test

import (
	"math"
	"testing"
	"time"

	"m31labs.dev/gosx/game/camera"
	"m31labs.dev/gosx/game/replay"
	"m31labs.dev/gosx/room/roomtest"
)

func dist(a, b camera.Vec3) float64 { return a.Sub(b).Len() }

func newRig(g *roomtest.Game) *replay.Rig {
	return replay.NewRig(replay.RigConfig{
		Subject: func(float64) (camera.Vec3, bool) {
			b := g.World.Bodies[0]
			return camera.Vec3{X: float64(b.X) / roomtest.Unit, Z: float64(b.Y) / roomtest.Unit}, true
		},
		Shots: map[string]camera.Pose{
			"overhead": {Position: camera.Vec3{Y: 40, Z: 1}, Target: camera.Vec3{}},
			"sideline": {Position: camera.Vec3{X: 30, Y: 5}, Target: camera.Vec3{}},
		},
		Near: 0.1, Far: 500,
	})
}

func TestRigModesBlendWithoutPop(t *testing.T) {
	g := newGame()
	r := newRig(g)
	var pose camera.Pose
	for i := 0; i < 120; i++ {
		pose = r.Update(1.0/60, 0)
	}
	if !pose.Valid() {
		t.Fatalf("broadcast pose invalid: %+v", pose)
	}
	before := pose.Position
	r.SetMode(replay.Orbit)
	first := r.Update(1.0/60, 0)
	if d := dist(first.Position, before); d > 0.5 {
		t.Fatalf("switch to orbit popped %v units", d)
	}
	// Orbit: turning moves the camera but keeps the distance to the target.
	for i := 0; i < 60; i++ {
		pose = r.Update(1.0/60, 0)
	}
	d0 := dist(pose.Position, pose.Target)
	r.Look(0.5, 0.1)
	for i := 0; i < 120; i++ {
		pose = r.Update(1.0/60, 0)
	}
	if dist(pose.Position, before) < 1 {
		t.Fatal("look did not move the orbit camera")
	}
	if d := dist(pose.Position, pose.Target); math.Abs(d-d0) > 0.01 {
		t.Fatalf("orbit distance changed from %v to %v", d0, d)
	}
	r.Zoom(math.Log(2))
	pose = r.Update(1.0/60, 0)
	pose = r.Update(1.0/60, 0)
	if d := dist(pose.Position, pose.Target); math.Abs(d-2*d0) > 0.1*d0 {
		t.Fatalf("zoom: distance %v, want about %v", d, 2*d0)
	}
	// Fly: forward input moves along the view direction.
	r.SetMode(replay.Fly)
	for i := 0; i < 90; i++ {
		pose = r.Update(1.0/60, 0)
	}
	start, fwd := pose.Position, pose.Forward()
	r.Move(1, 0, 0)
	for i := 0; i < 60; i++ {
		pose = r.Update(1.0/60, 0)
	}
	moved := pose.Position.Sub(start)
	if moved.Len() < 6 || moved.Norm().Dot(fwd) < 0.99 {
		t.Fatalf("fly moved %v along %v", moved, fwd)
	}
	r.Move(0, 0, 0)
	r.SetMode(replay.Broadcast)
	for i := 0; i < 240; i++ {
		pose = r.Update(1.0/60, 0)
	}
	if r.Mode() != replay.Broadcast || !pose.Valid() {
		t.Fatalf("back to broadcast: %v %+v", r.Mode(), pose)
	}
	cam := r.Camera()
	if cam.Near != 0.1 || cam.Far != 500 || cam.FOV <= 0 {
		t.Fatalf("scene camera %+v", cam)
	}
}

func TestRigShots(t *testing.T) {
	g := newGame()
	r := newRig(g)
	for i := 0; i < 10; i++ {
		r.Update(1.0/60, 0)
	}
	var seen []string
	for i := 0; i < 4; i++ {
		seen = append(seen, r.NextShot(1))
	}
	want := []string{"overhead", "sideline", "", "overhead"}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("cycle %v, want %v", seen, want)
		}
	}
	r.ShowShot("sideline")
	var pose camera.Pose
	for i := 0; i < 200; i++ {
		pose = r.Update(1.0/60, 0)
	}
	if dist(pose.Position, camera.Vec3{X: 30, Y: 5}) > 0.05 {
		t.Fatalf("sideline pose %+v", pose)
	}
	if r.ShowShot("nope") || r.Shot() != "" {
		t.Fatal("unknown shot should return to chase")
	}
	if r.NextShot(-1) != "sideline" {
		t.Fatal("reverse cycle")
	}
}

func TestInterpolatorBlendsAndSnapsOnSeek(t *testing.T) {
	rec, _ := play(t, 100, replay.Config{Interval: 20})
	g := newGame()
	p, _ := replay.NewPlayer(g, rec.Recording())
	var in replay.Interpolator
	sample := func(dst []float64) []float64 {
		return append(dst, float64(g.World.Bodies[0].X))
	}
	var buf []float64
	p.Resume()
	p.Advance(time.Second / 60)
	in.Update(p, sample)
	x1 := float64(g.World.Bodies[0].X)
	p.Advance(time.Second / 60)
	in.Update(p, sample)
	x2 := float64(g.World.Bodies[0].X)
	if x1 == x2 {
		t.Skip("body did not move")
	}
	buf = in.At(0.5, buf)
	if math.Abs(buf[0]-(x1+x2)/2) > 1e-9 {
		t.Fatalf("blend %v, want %v", buf[0], (x1+x2)/2)
	}
	_ = p.Seek(50)
	in.Update(p, sample)
	buf = in.At(0.5, buf)
	if buf[0] != float64(g.World.Bodies[0].X) {
		t.Fatal("seek should show the new state unblended")
	}
}
