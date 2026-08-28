package preview

import (
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

func TestPreviewMaterialPropagatesAuthoredIOR(t *testing.T) {
	ior := 2.42
	m := materialFromObject(scene.ObjectIR{Color: "#ffffff", IOR: &ior})
	if m.IOR == nil || *m.IOR != 2.42 {
		t.Fatalf("authored IOR was not propagated: %+v", m.IOR)
	}
	if !strings.Contains(m.Key, "2.42") {
		t.Fatalf("material key %q is missing the authored ior", m.Key)
	}

	fromInstance := materialFromInstance(scene.InstancedMeshIR{Color: "#ffffff", IOR: &ior})
	if fromInstance.IOR == nil || *fromInstance.IOR != 2.42 {
		t.Fatalf("instanced IOR was not propagated: %+v", fromInstance.IOR)
	}

	def := materialFromObject(scene.ObjectIR{Color: "#ffffff"})
	if def.IOR != nil {
		t.Fatal("an absent IOR must stay absent, not be synthesized to 1.5")
	}
	if m.Key == def.Key {
		t.Fatal("an authored IOR must produce a distinct material key")
	}
}

func TestPreviewInvalidIORDropsFieldWithoutPoisoningKey(t *testing.T) {
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1, 0.5} {
		v := bad
		m := materialFromObject(scene.ObjectIR{Color: "#ffffff", IOR: &v})
		if m.IOR != nil {
			t.Fatalf("ior %v must not reach the render material", bad)
		}
		if m.Key == "" {
			t.Fatalf("ior %v poisoned the JSON material key", bad)
		}
	}
	// An explicit zero is honored, not dropped.
	zero := 0.0
	if m := materialFromObject(scene.ObjectIR{Color: "#ffffff", IOR: &zero}); m.IOR == nil {
		t.Fatal("an explicit zero IOR must be honored")
	}
}
