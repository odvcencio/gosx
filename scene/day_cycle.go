package scene

import (
	"fmt"
	"math"
)

// DayCycle advances a physical sky on the Scene3D animation clock. It pauses
// with the scene, has no independent browser timer, and preserves simulations.
// DurationSeconds defaults to 1080 (18 minutes). InitialHour is local solar time.
type DayCycle struct {
	DurationSeconds   float64 `json:"durationSeconds,omitempty"`
	InitialHour       float64 `json:"initialHour"`
	SunLightID        string  `json:"sunLightId"`
	SunAzimuth        float64 `json:"sunAzimuth,omitempty"`        // azimuth at noon; degrees
	PeakElevation     float64 `json:"peakElevation,omitempty"`     // default 55 degrees
	SunIntensity      float64 `json:"sunIntensity,omitempty"`      // default 9
	NightLightID      string  `json:"nightLightId,omitempty"`      // optional existing directional light, opposite the sun
	NightIntensity    float64 `json:"nightIntensity,omitempty"`    // default 0.22; fades out at dawn
	NightEnvIntensity float64 `json:"nightEnvIntensity,omitempty"` // default 0.025; ambient floor at night
	Seed              uint32  `json:"seed,omitempty"`
	Weather           bool    `json:"weather,omitempty"`     // smoothly varying cloud cover and haze
	InputSignal       string  `json:"inputSignal,omitempty"` // true runs, false holds the sky
	OutputSignal      string  `json:"outputSignal,omitempty"`
}

type DayState struct {
	Hour                    float64
	HourLabel, WeatherLabel string
	Automatic               bool
}

func (d DayCycle) Validate() error {
	for _, v := range []float64{d.DurationSeconds, d.InitialHour, d.SunAzimuth, d.PeakElevation, d.SunIntensity, d.NightIntensity, d.NightEnvIntensity} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("day cycle values must be finite")
		}
	}
	if d.DurationSeconds != 0 && (d.DurationSeconds < 60 || d.DurationSeconds > 86400) {
		return fmt.Errorf("day cycle duration must be within 60..86400 seconds")
	}
	if d.InitialHour < 0 || d.InitialHour >= 24 || d.PeakElevation < 0 || d.PeakElevation > 89 || d.SunIntensity < 0 || d.SunIntensity > 30 {
		return fmt.Errorf("day cycle solar values out of range")
	}
	if d.NightIntensity < 0 || d.NightIntensity > 30 || d.NightEnvIntensity < 0 || d.NightEnvIntensity > 4 {
		return fmt.Errorf("day cycle night lighting values out of range")
	}
	return nil
}

// RuntimeMetrics reports observed renderer submissions and browser transfer entries.
// TransferLabel may exclude cached or opaque cross-origin transfers.
type RuntimeMetrics struct {
	Ready                   bool
	TransferLabel, FPSLabel string
}
