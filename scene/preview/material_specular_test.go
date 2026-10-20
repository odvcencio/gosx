package preview

import (
	"math"
	"testing"

	"m31labs.dev/gosx/scene"
)

func TestPreviewMaterialPropagatesSpecularFields(t *testing.T) {
	i := 0.5
	c := [3]float64{100, 1, 0}
	m := materialFromObject(scene.ObjectIR{Color: "#ffffff", SpecularIntensity: &i, SpecularColor: &c})
	if m.SpecularIntensity == nil || *m.SpecularIntensity != 0.5 {
		t.Fatalf("authored specular intensity was not propagated: %+v", m.SpecularIntensity)
	}
	if m.SpecularColor == nil || *m.SpecularColor != [3]float64{100, 1, 0} {
		t.Fatalf("authored specular colour was not propagated: %+v", m.SpecularColor)
	}

	fromInstance := materialFromInstance(scene.InstancedMeshIR{Color: "#ffffff", SpecularIntensity: &i, SpecularColor: &c})
	if fromInstance.SpecularIntensity == nil || *fromInstance.SpecularIntensity != 0.5 {
		t.Fatalf("instanced specular intensity was not propagated: %+v", fromInstance.SpecularIntensity)
	}
	if fromInstance.SpecularColor == nil || *fromInstance.SpecularColor != [3]float64{100, 1, 0} {
		t.Fatalf("instanced specular colour was not propagated: %+v", fromInstance.SpecularColor)
	}

	// Absence stays absence.
	def := materialFromObject(scene.ObjectIR{Color: "#ffffff"})
	if def.SpecularIntensity != nil || def.SpecularColor != nil {
		t.Fatal("absent specular fields must stay nil, not be synthesized")
	}

	// Both fields participate in the material key.
	onlyIntensity := materialFromObject(scene.ObjectIR{Color: "#ffffff", SpecularIntensity: &i})
	onlyColor := materialFromObject(scene.ObjectIR{Color: "#ffffff", SpecularColor: &c})
	if m.Key == def.Key || m.Key == onlyIntensity.Key || m.Key == onlyColor.Key ||
		onlyIntensity.Key == def.Key || onlyColor.Key == def.Key {
		t.Fatal("specular intensity and colour must produce distinct material keys")
	}
}

func TestPreviewInvalidSpecularDroppedWithoutPoisoningKey(t *testing.T) {
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -0.1, 1.5} {
		v := bad
		m := materialFromObject(scene.ObjectIR{Color: "#ffffff", SpecularIntensity: &v})
		if m.SpecularIntensity != nil {
			t.Fatalf("specular intensity %v must not reach the render material", bad)
		}
		if m.Key == "" {
			t.Fatalf("specular intensity %v poisoned the JSON material key", bad)
		}
	}
	for _, bad := range [][]float64{{math.NaN(), 1, 1}, {1, math.Inf(1), 1}, {1, 1, math.Inf(-1)}, {-1, 1, 1}, {1, -0.5, 1}} {
		c := [3]float64{bad[0], bad[1], bad[2]}
		m := materialFromObject(scene.ObjectIR{Color: "#ffffff", SpecularColor: &c})
		if m.SpecularColor != nil {
			t.Fatalf("specular colour %v must not reach the render material", bad)
		}
		if m.Key == "" {
			t.Fatalf("specular colour %v poisoned the JSON material key", bad)
		}
	}
	// Explicit zeros are honoured, not dropped.
	zero := 0.0
	if m := materialFromObject(scene.ObjectIR{Color: "#ffffff", SpecularIntensity: &zero}); m.SpecularIntensity == nil {
		t.Fatal("an explicit zero specular intensity must be honoured")
	}
	black := [3]float64{0, 0, 0}
	if m := materialFromObject(scene.ObjectIR{Color: "#ffffff", SpecularColor: &black}); m.SpecularColor == nil {
		t.Fatal("an explicit black specular colour must be honoured")
	}
	// HDR above 1 is honoured.
	hdr := [3]float64{2, 0.4, 100}
	if m := materialFromObject(scene.ObjectIR{Color: "#ffffff", SpecularColor: &hdr}); m.SpecularColor == nil {
		t.Fatal("an HDR specular colour must be honoured")
	}
}

func TestPreviewSpecularPointersSnapshot(t *testing.T) {
	i := 0.5
	c := [3]float64{1, 0.5, 2}
	m := materialFromObject(scene.ObjectIR{Color: "#ffffff", SpecularIntensity: &i, SpecularColor: &c})
	// Mutate the originals after the material was minted, even to values that
	// would now be considered invalid.
	i = 1.5
	c = [3]float64{math.NaN(), math.NaN(), math.NaN()}
	if m.SpecularIntensity == nil || *m.SpecularIntensity != 0.5 {
		t.Fatalf("intensity pointer was not snapshotted: %+v", m.SpecularIntensity)
	}
	if m.SpecularColor == nil || *m.SpecularColor != [3]float64{1, 0.5, 2} {
		t.Fatalf("colour pointer was not snapshotted: %+v", m.SpecularColor)
	}
	if m.Key == "" {
		t.Fatal("snapshot mutation poisoned the JSON material key")
	}
}
