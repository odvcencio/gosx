package cycle

import (
	"fmt"
	"math"
	"strings"
)

// RenderState links an operating point to a schematic display. It neither
// predicts metal temperature nor integrates shaft inertia. The single speed
// reduction preserves the solved N2/N1 ratio without aliasing at real RPM.
type RenderState struct {
	ThermalTint      bool    `json:"thermalTint"`
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

const SurfaceLinerColor = "#8b8172"

func Visualize(mode string, r Result, mapped MappedResult, err error) RenderState {
	v := RenderState{Mode: mode, Valid: err == nil, Color: SurfaceLinerColor, LowLabel: "—", HighLabel: "—", TemperatureLabel: "—", Caption: "No solution · rotors held"}
	if err != nil {
		return v
	}
	v.LowSpin, v.HighSpin = .48, .82
	v.LowLabel, v.HighLabel = "illustrative", "illustrative"
	v.Caption = "Illustrative rotor speeds · studio materials"
	if mode == "mapped" {
		v.N1RPM, v.N2RPM = mapped.N1RPM, mapped.N2RPM
		v.LowSpin = v.N1RPM * (2 * math.Pi / 60 / DisplaySpeedReduction)
		v.HighSpin = v.N2RPM * (2 * math.Pi / 60 / DisplaySpeedReduction)
		v.LowLabel, v.HighLabel = fmt.Sprintf("%.0f rpm", v.N1RPM), fmt.Sprintf("%.0f rpm", v.N2RPM)
		v.Caption = "Solved spools · rotation at 1/800 speed · studio materials"
	}
	v.TemperatureK = r.Burner.TemperatureK
	v.TemperatureLabel = fmt.Sprintf("%.0f K", v.TemperatureK)
	v.Color = SurfaceLinerColor
	return v
}

// WithThermalTint is an optional gas-temperature diagnostic, not a metal-temperature model.
func (v RenderState) WithThermalTint(enabled bool) RenderState {
	v.ThermalTint = enabled
	v.Color = SurfaceLinerColor
	v.Caption = strings.TrimSuffix(v.Caption, " · gas-temperature diagnostic")
	if enabled && v.Valid {
		heat := math.Max(0, math.Min(1, (v.TemperatureK-700)/1000))
		v.Color = fmt.Sprintf("#%02x%02x%02x", int(math.Round(72+183*heat)), int(math.Round(146-50*heat)), int(math.Round(191-145*heat)))
		v.Caption += " · gas-temperature diagnostic"
	}
	return v
}
