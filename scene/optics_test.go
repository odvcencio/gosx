package scene

import (
	"math"
	"testing"
)

func TestOpticsValidatesBoundedWorldSurvey(t *testing.T) {
	valid := Optics{Magnification: 3, InputSignal: "$coast.optics", OutputSignal: "$coast.sight", Landmarks: []Landmark{{ID: "island", Name: "Island", Position: Vec3(120, 0, -200), Landing: true}}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, magnification := range []float64{math.NaN(), math.Inf(1), .5, 9} {
		invalid := valid
		invalid.Magnification = magnification
		if invalid.Validate() == nil {
			t.Fatal("accepted invalid magnification")
		}
	}
	duplicate := valid
	duplicate.Landmarks = append(append([]Landmark{}, valid.Landmarks...), valid.Landmarks[0])
	if duplicate.Validate() == nil {
		t.Fatal("ambiguous landmark identifiers must fail")
	}
	invalid := valid
	invalid.Landmarks = []Landmark{{ID: "island", Name: "Island", Position: Vec3(math.NaN(), 0, 0)}}
	if invalid.Validate() == nil {
		t.Fatal("accepted non-finite surveyed position")
	}
	if _, exists := (Props{}).GoSXSpreadProps()["optics"]; exists {
		t.Fatal("unconfigured scenes must not carry instrument payload")
	}
	if (Props{Optics: &valid}).GoSXSpreadProps()["optics"] != &valid {
		t.Fatal("typed scene authoring lost its optical instrument")
	}
}
