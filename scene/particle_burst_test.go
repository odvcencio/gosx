package scene

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
	"time"
)

func testParticleBurst() ParticleBurst {
	return ParticleBurst{ID: "impact", Count: 6, Delay: 190 * time.Millisecond,
		Emitter:  ParticleEmitter{Position: Vec3(1, .05, 2), Lifetime: .4},
		Material: ParticleMaterial{Color: "#cfbd89", Size: .08, Opacity: .65}}
}

func burstCommandRecords(t *testing.T, value any) []ComputeParticlesIR {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var records []ComputeParticlesIR
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	return records
}

func TestParticleBurstWireFixture(t *testing.T) {
	data, err := json.Marshal(testParticleBurst())
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/particle_burst.json")
	if err != nil {
		t.Fatal(err)
	}
	var got, want particleBurstJSON
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fixture, &want); err != nil {
		t.Fatal(err)
	}
	if math.Abs(got.Duration-want.Duration) > 1e-8 {
		t.Fatalf("deadline: %v", got.Duration)
	}
	got.Duration = want.Duration
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wire: %s", data)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	material := wire["particles"].(map[string]any)["material"].(map[string]any)
	for _, key := range []string{"sizeEnd", "opacityEnd"} {
		if value, exists := material[key]; !exists || value != float64(0) {
			t.Fatalf("wire lost zero fade endpoint %s", key)
		}
	}
}

func TestParticleBurstCapabilityIsOptIn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		props := Props{}
		if enabled {
			props.ParticleBursts = Bool(true)
		}
		var wire map[string]any
		payload, err := json.Marshal(props)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(payload, &wire); err != nil {
			t.Fatal(err)
		}
		if enabled && wire["particleBursts"] != true {
			t.Fatal("typed capability was lost")
		}
		if !enabled {
			if _, exists := wire["particleBursts"]; exists {
				t.Fatal("ordinary scenes must not carry the capability")
			}
		}
	}
}

func TestParticleBurstPreservesLayersAndExpires(t *testing.T) {
	b := testParticleBurst()
	base := SceneIR{Points: []PointsIR{{ID: "stars"}}, ComputeParticles: []ComputeParticlesIR{{ID: "smoke", Count: 2}}, WaterSystems: []WaterSystemIR{{ID: "water"}}}
	for _, tc := range []struct {
		seconds float64
		count   int
	}{{0, 1}, {.19, 2}, {.4, 2}, {b.Duration().Seconds(), 1}, {5, 1}} {
		command, err := b.Sample(base, tc.seconds)
		if err != nil {
			t.Fatal(err)
		}
		data := command.Data.(map[string]any)
		if command.Kind != CommandSetParticles || len(burstCommandRecords(t, data["computeParticles"])) != tc.count ||
			!reflect.DeepEqual(data["points"], base.Points) || !reflect.DeepEqual(data["waterSystems"], base.WaterSystems) {
			t.Fatalf("sample at %v: %#v", tc.seconds, command)
		}
	}
	active, _ := b.Sample(base, .3)
	base.ComputeParticles = burstCommandRecords(t, active.Data.(map[string]any)["computeParticles"])
	resampled, _ := b.Sample(base, .4)
	if len(burstCommandRecords(t, resampled.Data.(map[string]any)["computeParticles"])) != 2 || len(base.ComputeParticles) != 2 {
		t.Fatal("sampling duplicates or mutates the base layers")
	}
	if !base.ComputeParticles[1].Emitter.Once {
		t.Fatal("burst must always be one-shot")
	}
}

func TestParticleBurstRejectsInvalidPlans(t *testing.T) {
	cases := []func(*ParticleBurst){
		func(b *ParticleBurst) { b.Count = 0 }, func(b *ParticleBurst) { b.Count = 4097 },
		func(b *ParticleBurst) { b.ID = " " }, func(b *ParticleBurst) { b.Delay = -1 },
		func(b *ParticleBurst) { b.Delay = 61 * time.Second }, func(b *ParticleBurst) { b.Emitter.Lifetime = 0 },
		func(b *ParticleBurst) { b.Emitter.Lifetime = math.NaN() }, func(b *ParticleBurst) { b.Emitter.Kind = "unknown" },
		func(b *ParticleBurst) { b.Emitter.Position.X = math.Inf(1) }, func(b *ParticleBurst) { b.Material.Opacity = 1.1 },
		func(b *ParticleBurst) { b.Material.Size = -1 }, func(b *ParticleBurst) { b.Forces = []ParticleForce{{Kind: ""}} },
		func(b *ParticleBurst) { b.Forces = []ParticleForce{{Kind: "gravity", Strength: math.Inf(1)}} },
	}
	for i, mutate := range cases {
		b := testParticleBurst()
		mutate(&b)
		if _, err := json.Marshal(b); err == nil {
			t.Errorf("case %d marshaled invalid burst", i)
		}
		if _, err := b.Sample(SceneIR{}, 0); err == nil {
			t.Errorf("case %d sampled invalid burst", i)
		}
	}
	if _, err := testParticleBurst().Sample(SceneIR{}, math.NaN()); err == nil {
		t.Fatal("accepted non-finite time")
	}
}
