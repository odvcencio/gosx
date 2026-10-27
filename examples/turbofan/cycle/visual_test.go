package cycle

import "testing"

func TestThermalTintIsAnIndependentDiagnostic(t *testing.T) {
	m, err := JT9D()
	if err != nil {
		t.Fatal(err)
	}
	var surfaces []RenderState
	for _, fuel := range []float64{.2054, 2.055} {
		r, err := m.Solve(MappedInputs{FuelFlowKGS: fuel})
		if err != nil {
			t.Fatal(err)
		}
		v := Visualize("mapped", r.Cycle, r, nil)
		thermal := v.WithThermalTint(true)
		if v.ThermalTint || v.Color != SurfaceLinerColor || !thermal.ThermalTint || thermal.Color == v.Color {
			t.Fatal("surface finish and temperature diagnostic are coupled")
		}
		if thermal.LowSpin != v.LowSpin || thermal.HighSpin != v.HighSpin || thermal.TemperatureK != v.TemperatureK || thermal.N1RPM != v.N1RPM || thermal.N2RPM != v.N2RPM {
			t.Fatal("material toggle changed the operating point or shaft motion")
		}
		if restored := thermal.WithThermalTint(false); restored != v {
			t.Fatal("turning tint off did not restore the original surface and caption")
		}
		surfaces = append(surfaces, v)
	}
	if surfaces[0].Color != surfaces[1].Color {
		t.Fatal("fuel changes recolor the default metal finish")
	}
	if surfaces[0].WithThermalTint(true).Color == surfaces[1].WithThermalTint(true).Color {
		t.Fatal("the optional gas-temperature diagnostic lost its temperature response")
	}
}
