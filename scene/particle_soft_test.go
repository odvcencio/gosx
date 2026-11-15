package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestParticleMaterialSoftDistanceLowers(t *testing.T) {
	graph := NewGraph(ComputeParticles{ID: "spray", Count: 64,
		Emitter:  ParticleEmitter{Kind: "disc", Radius: 2, Lifetime: 1.5},
		Material: ParticleMaterial{Color: "#e8f4ff", Size: 0.3, Opacity: 0.6, BlendMode: BlendAlpha, SoftDistance: 0.75}})
	ir := graph.SceneIR()
	if len(ir.ComputeParticles) != 1 || ir.ComputeParticles[0].Material.SoftDistance != 0.75 {
		t.Fatalf("scene IR lost softDistance: %+v", ir.ComputeParticles)
	}
	data, err := json.Marshal(ir.ComputeParticles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"softDistance":0.75`) {
		t.Fatalf("wire lost softDistance: %s", data)
	}
	node := computeToIRNode(ir.ComputeParticles[0])
	if node.Compute.Material.SoftDistance != 0.75 {
		t.Fatalf("canonical IR lost softDistance: %+v", node.Compute.Material)
	}
	hard := NewGraph(ComputeParticles{ID: "embers", Count: 8, Material: ParticleMaterial{Size: 1}}).SceneIR()
	data, _ = json.Marshal(hard.ComputeParticles[0])
	if strings.Contains(string(data), "softDistance") {
		t.Fatalf("zero softDistance must stay off the wire: %s", data)
	}
}

func TestParticleMaterialSoftDistanceTransitionState(t *testing.T) {
	props := &ComputeParticlesProps{Material: &ParticleMaterial{BlendMode: BlendAdditive, SoftDistance: 0.4}}
	record := props.legacyProps()
	material, _ := record["material"].(map[string]any)
	if material["softDistance"] != 0.4 {
		t.Fatalf("transition state lost softDistance: %#v", record)
	}
}

func TestParticleBurstRejectsInvalidSoftDistance(t *testing.T) {
	for _, value := range []float64{-0.1, math.NaN(), math.Inf(1)} {
		b := testParticleBurst()
		b.Material.SoftDistance = value
		if _, err := json.Marshal(b); err == nil {
			t.Errorf("softDistance %v marshaled", value)
		}
	}
	b := testParticleBurst()
	b.Material.BlendMode = BlendAlpha
	b.Material.SoftDistance = 0.25
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"softDistance":0.25`) {
		t.Fatalf("burst wire lost softDistance: %s", data)
	}
}
