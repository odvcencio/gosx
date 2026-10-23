package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func cameraCommandOrbit(t *testing.T, commands []Command) map[string]any {
	t.Helper()
	for _, command := range commands {
		if command.Kind != CommandSetCamera {
			continue
		}
		raw, err := json.Marshal(command.Data)
		if err != nil {
			t.Fatal(err)
		}
		var camera struct{ Orbit map[string]any }
		if err = json.Unmarshal(raw, &camera); err != nil {
			t.Fatal(err)
		}
		return camera.Orbit
	}
	return nil
}

func TestSetCameraCommandCarriesOrbitAndKeepsTheOriginAsATarget(t *testing.T) {
	command := SetCameraCommand(PerspectiveCamera{Position: Vec3(0, 10, 30), FOV: 50, Orbit: &Orbit{Target: Vec3(0, 0, 0), MaxDistance: 80}})
	raw, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	// A target at the origin must still be present: the runtime keeps the old
	// target for a coordinate that is missing, so omitting zeros would drop the move.
	if !strings.Contains(string(raw), `"orbit":{"target":{"x":0,"y":0,"z":0},"maxDistance":80}`) {
		t.Fatalf("orbit payload = %s", raw)
	}
	plain, _ := json.Marshal(SetCameraCommand(PerspectiveCamera{FOV: 50}))
	if strings.Contains(string(plain), "orbit") {
		t.Fatalf("camera without Orbit gained an orbit field: %s", plain)
	}
	var back Orbit
	if err = json.Unmarshal([]byte(`{"target":{"x":1,"y":2,"z":3},"minDistance":4,"pitchLimit":0.5}`), &back); err != nil || back != (Orbit{Target: Vec3(1, 2, 3), MinDistance: 4, PitchLimit: 0.5}) {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
}

func TestOrbitSurvivesTheIRCameraRoundTrip(t *testing.T) {
	camera := PerspectiveCamera{Position: Vec3(1, 2, 3), FOV: 40, Orbit: &Orbit{Target: Vec3(5, 6, 7)}}
	ir := CameraIR(camera)
	camera.Orbit.Target.X = 99 // the record owns a copy
	if ir.Orbit == nil || ir.Orbit.Target.X != 5 {
		t.Fatalf("IR orbit = %+v", ir.Orbit)
	}
	if got := ir.Perspective().Orbit; got == nil || got.Target != Vec3(5, 6, 7) {
		t.Fatalf("Perspective orbit = %+v", got)
	}
}

func TestChangingTheControlTargetEmitsACameraCommandWithTheNewOrbit(t *testing.T) {
	previous := Props{Controls: ControlOrbit, ControlTarget: Vec3(0, 7, -6), ControlMinDistance: 20, Camera: PerspectiveCamera{Position: Vec3(104, 40, 124), FOV: 42}}
	next := previous
	next.ControlTarget = Vec3(0, 20, -260)
	next.ControlMaxDistance = 300
	orbit := cameraCommandOrbit(t, DiffIRCommands(previous.CanonicalIR(), next.CanonicalIR()))
	if orbit == nil {
		t.Fatal("a changed control target produced no camera command")
	}
	target, _ := orbit["target"].(map[string]any)
	if target["y"] != float64(20) || target["z"] != float64(-260) || orbit["maxDistance"] != float64(300) || orbit["minDistance"] != float64(20) {
		t.Fatalf("orbit = %v", orbit)
	}
	if cameraCommandOrbit(t, DiffIRCommands(next.CanonicalIR(), next.CanonicalIR())) != nil {
		t.Fatal("an unchanged scene emitted a camera command")
	}
	next.Controls = ""
	if cameraCommandOrbit(t, DiffIRCommands(Props{Camera: next.Camera}.CanonicalIR(), next.CanonicalIR())) != nil {
		t.Fatal("a scene without orbit controls carries an orbit")
	}
}

func TestOrbitPitchRangeAndFloorSerializeAndCopy(t *testing.T) {
	orbit := Orbit{Target: Vec3(0, 5, 0), PitchLimit: 1.4, MinPitch: Float(0), MaxPitch: Float(0.9), MinHeight: Float(2)}
	raw, err := json.Marshal(SetCameraCommand(PerspectiveCamera{FOV: 50, Orbit: &orbit}))
	if err != nil {
		t.Fatal(err)
	}
	// MinPitch of zero is a value, not an omission.
	for _, want := range []string{`"minPitch":0`, `"maxPitch":0.9`, `"minHeight":2`, `"pitchLimit":1.4`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s in %s", want, raw)
		}
	}
	plain, _ := json.Marshal(Orbit{Target: Vec3(1, 2, 3)})
	for _, field := range []string{"minPitch", "maxPitch", "minHeight"} {
		if strings.Contains(string(plain), field) {
			t.Fatalf("unset %s was sent: %s", field, plain)
		}
	}
	var back Orbit
	if err = json.Unmarshal(plain, &back); err != nil || back.MinPitch != nil {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
	ir := CameraIR(PerspectiveCamera{Orbit: &orbit})
	*orbit.MinPitch = -1 // the camera record owns copies of the pointers
	if ir.Orbit.MinPitch == nil || *ir.Orbit.MinPitch != 0 {
		t.Fatalf("IR shares the caller's MinPitch: %v", ir.Orbit.MinPitch)
	}
}
