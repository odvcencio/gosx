package vessel_test

import (
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/det/parity"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/vessel"
)

// linerBench runs the powered liner through a storm and returns what a game
// would send to the browser at 10 Hz: one pose per tick. The 60 Hz steps and the
// scripted course changes are fixed, so the poses are the same on every platform
// (TestLinerBenchParity pins them with a golden trace).
func linerBench() ([]scene.VesselPose, parity.Trace) {
	sea := vessel.NewSea(vessel.SeaConfig{WaveHeight: 7, WaveLength: 80, WindDirection: 60})
	ship := vessel.NewShip(vessel.Liner(), 0, 0, 0, sea)
	rec := parity.NewRecorder("liner-bench", ship.AppendState)
	const dt = 1.0 / 60
	step := func(heading float64) {
		ship.Step(vessel.Input{Power: 0.8, Heading: heading, Stabilizers: false}, dt)
	}
	for i := 0; i < 120*60; i++ { // two minutes to reach speed
		step(0)
	}
	var poses []scene.VesselPose
	for tick := 0; tick < 80; tick++ {
		heading := 0.0
		switch {
		case tick >= 20 && tick < 55:
			heading = -0.35 // a long turn to port: the ship heels outward
		case tick >= 55:
			heading = 0.1
		}
		for k := 0; k < 6; k++ {
			step(heading)
			rec.Step()
		}
		p, st := ship.Pose(), ship.State
		poses = append(poses, scene.VesselPose{ID: "liner", Time: st.Time, X: p.X, Y: p.Y, Z: p.Z,
			Heading: float32(p.Heading), Pitch: float32(p.Pitch), Roll: float32(p.Roll), Speed: float32(st.Speed)})
	}
	return poses, rec.Trace()
}

func TestLinerBenchParity(t *testing.T) {
	_, trace := linerBench()
	path := filepath.Join("testdata", "parity-liner-bench.json")
	if f := flag.Lookup("update-parity"); f != nil && f.Value.String() == "true" {
		data, err := trace.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update-parity to record)", err)
	}
	want, err := parity.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if d := parity.Compare(want, trace); d != nil {
		t.Error(d)
	}
	t.Logf("liner-bench trace digest %s", trace.Digest())
}

// TestLinerBenchFixture writes (with UPDATE_LINER_BENCH=1) and checks the pose
// trace that client/js/scene3d-vessel-powered.bench.mjs replays: 80 poses at
// 10 Hz of the liner in a storm, the byte sizes the Go encoders produce, and
// the digest of the whole 60 Hz state trace.
func TestLinerBenchFixture(t *testing.T) {
	poses, trace := linerBench()
	var rollMax, pitchMax float64
	rows := make([][8]float64, len(poses))
	for i, p := range poses {
		rows[i] = [8]float64{p.Time, p.X, p.Y, p.Z, float64(p.Heading), float64(p.Pitch), float64(p.Roll), float64(p.Speed)}
		rollMax = math.Max(rollMax, math.Abs(float64(p.Roll)))
		pitchMax = math.Max(pitchMax, math.Abs(float64(p.Pitch)))
	}
	frame, err := scene.EncodeVesselPoseFrame(nil, scene.VesselPoseFrame{Vessels: poses[:1]})
	if err != nil {
		t.Fatal(err)
	}
	// 5 percent of 2,900 agents change per tick.
	const agents, changed = 2900, 145
	updates := make([]scene.PartialMotionUpdate, changed)
	for i := range updates {
		updates[i] = scene.PartialMotionUpdate{Slot: i * (agents / changed), Motion: scene.MotionInstanceIR{
			PrevX: 1, NextX: 2, TPrev: 1, TNext: 1.1, PrevScaleX: 1, PrevScaleY: 1, PrevScaleZ: 1, NextScaleX: 1, NextScaleY: 1, NextScaleZ: 1,
			Animation: "Run", AnimationLoop: true, PlaybackRate: 1}}
	}
	partial, err := scene.EncodePartialMotionFrame(scene.PartialMotionFrame{Batches: []scene.PartialMotionBatch{{ID: "passengers", Size: agents, Updates: updates}}})
	if err != nil {
		t.Fatal(err)
	}
	cutaway, _ := json.Marshal(scene.VesselCutaway{Enabled: true, Deck: 8, Above: "fade"})
	fixture := struct {
		ID                string       `json:"id"`
		Hz                int          `json:"hz"`
		Poses             [][8]float64 `json:"poses"` // time, x, y, z, heading, pitch, roll, speed
		PoseFrameBytes    int          `json:"poseFrameBytes"`
		PartialFrameBytes int          `json:"partialFrameBytes"`
		Agents            int          `json:"agents"`
		Changed           int          `json:"changed"`
		CutawayJSONBytes  int          `json:"cutawayJsonBytes"`
		StateDigest       string       `json:"stateDigest"`
	}{"liner", 10, rows, len(frame), len(partial), agents, changed, len(cutaway), trace.Digest().String()}
	got, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "client", "js", "testdata", "liner-bench-trace.json")
	if os.Getenv("UPDATE_LINER_BENCH") != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(got) {
		t.Fatal("liner bench fixture is stale; rerun with UPDATE_LINER_BENCH=1")
	}
	if rollMax < 0.01 || pitchMax < 0.005 {
		t.Errorf("the storm should visibly roll the liner: max roll %.4f rad, pitch %.4f rad", rollMax, pitchMax)
	}
	t.Logf("liner bench: %d poses, max roll %.2f deg, max pitch %.2f deg, pose frame %d B, partial frame %d B for %d of %d agents, cutaway JSON %d B",
		len(poses), rollMax*180/math.Pi, pitchMax*180/math.Pi, len(frame), len(partial), changed, agents, len(cutaway))
}
