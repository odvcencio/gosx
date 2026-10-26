package cycle

import (
	"fmt"
	"math"
)

// RenderState links an operating point to a schematic display. It neither
// predicts metal temperature nor integrates shaft inertia. The single speed
// reduction preserves the solved N2/N1 ratio without aliasing at real RPM.
type RenderState struct {
	Mode             string  `json:"mode"`
	Valid            bool    `json:"valid"`
	LowSpin          float64 `json:"lowSpin"`
	HighSpin         float64 `json:"highSpin"`
	N1RPM            float64 `json:"n1RPM"`
	N2RPM            float64 `json:"n2RPM"`
	TemperatureK     float64 `json:"temperatureK"`
	Color            string  `json:"color"`
	LowLabel         string  `json:"lowLabel"`
	HighLabel        string  `json:"highLabel"`
	TemperatureLabel string  `json:"temperatureLabel"`
	Caption          string  `json:"caption"`
}

const DisplaySpeedReduction = 800.0

func Visualize(mode string, r Result, mapped MappedResult, err error) RenderState {
	v := RenderState{Mode: mode, Valid: err == nil, Color: "#637c8c", LowLabel: "—", HighLabel: "—", TemperatureLabel: "—", Caption: "No solution · rotors held"}
	if err != nil {
		return v
	}
	v.LowSpin, v.HighSpin = .48, .82
	v.LowLabel, v.HighLabel = "illustrative", "illustrative"
	v.Caption = "Illustrative rotor speeds · gas-temperature tint"
	if mode == "mapped" {
		v.N1RPM, v.N2RPM = mapped.N1RPM, mapped.N2RPM
		v.LowSpin = v.N1RPM * (2 * math.Pi / 60 / DisplaySpeedReduction)
		v.HighSpin = v.N2RPM * (2 * math.Pi / 60 / DisplaySpeedReduction)
		v.LowLabel, v.HighLabel = fmt.Sprintf("%.0f rpm", v.N1RPM), fmt.Sprintf("%.0f rpm", v.N2RPM)
		v.Caption = "Solved spools · rotation at 1/800 speed · gas-temperature tint"
	}
	v.TemperatureK = r.Burner.TemperatureK
	v.TemperatureLabel = fmt.Sprintf("%.0f K", v.TemperatureK)
	heat := math.Max(0, math.Min(1, (v.TemperatureK-700)/1000))
	v.Color = fmt.Sprintf("#%02x%02x%02x", int(math.Round(72+183*heat)), int(math.Round(146-50*heat)), int(math.Round(191-145*heat)))
	return v
}
