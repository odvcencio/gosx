package scene

import "math"

// The Elio ocean pass graph (`elio emit ts --set ocean --backend webgpu`, kernel
// set "ocean") takes its settings as named value parameters. The generic graph
// runner fills those parameters from the normalized ocean, and it knows nothing
// about oceans: this file computes the values, so Go is the one place that
// decides what a spectrum, a choppiness, a presentation or a foam setting means
// for the graph. Normalization ships the result as Ocean.GraphParams and
// Ocean.GraphTween whenever Spectrum is set; both are computed and read-only,
// like SwellWaves.
//
// GraphParams holds exactly the graph's value-source parameters (frame, expr,
// accumulate and delta parameters are the graph's own):
//
//	windSpeed, fetch, gamma, spread, swell  the spectrum, with defaults applied
//	                                        (gamma is PeakEnhancement; fetch is km)
//	windDirDeg                              Ocean.WindDirection, degrees
//	swellDirDeg                             Spectrum.SwellDirection, degrees (0
//	                                        travels toward +Z, as WindDirection)
//	seed                                    Spectrum.Seed
//	loopPeriod                              1000 s: the kernels' loop
//	lambda                                  Choppiness, default 0.6
//	timeScale, driftX, driftZ               Presentation.TimeScale (default 1),
//	                                        WaterDriftX and WaterDriftZ (m/s)
//	foamTau, foamThreshold, foamGain        the foam constants below
//	probeCount, probeOriginX, probeOriginZ  the probe defaults: no points, at the origin
//	wakeEnabled                             1 when Wake has a NodeID and is Enabled, else 0
//	hullLength, hullBeam                    Wake.Length and Wake.Beam in metres (defaults 200 and 30)
//
// The hull's position, heading and speed are not parameters: the graph reads them
// from a scene node (a "node" parameter source named OceanGraphHullNode), and
// Ocean.GraphNodes maps that name to Wake.NodeID for the runner.
//
// The graph declares a build-time tween (20 s) on the parameters a spectrum
// change moves (windSpeed, fetch, gamma, spread, swell, windDirDeg, swellDirDeg,
// lambda). The tween is not a value parameter, so TransitionSeconds travels as
// Ocean.GraphTween (default 20) and the runner applies it to every tweened
// parameter: the runner stays generic and the app's setting wins.
//
// What the graph cannot take from here: the four default cascades are fixed in
// the graph, so Spectrum.Cascades is honoured by the Go swell waves but not by
// the graph; and probePoints (a vec4 array) is the vessel's, filled at run time.

// OceanFoam holds the foam constants, the single source for every renderer.
//
// Crests inject foam where the combined Jacobian J of the horizontal
// displacement drops below a threshold (J < 1 compresses the surface, J < 0
// folds it); the foam then persists for Tau seconds, which stretches each
// crest's injections into streaks. The threshold rises with wind speed U (m/s):
// zero at WindLow and below, so a glassy sea carries no foam, and MaxThreshold
// from WindHigh up, so whitecaps start near 10 m/s:
//
//	threshold1(U) = MaxThreshold * smoothstep(WindLow, WindHigh, U)
//
// threshold1 is the threshold for choppiness 1. J scales with choppiness, so
// the shipped threshold keeps the same crest compression at the authored
// choppiness L to first order: 1 - L (1 - threshold1(U)). A calm sea
// (U <= WindLow) has threshold 0 and no foam whatever the choppiness. Gain is
// per 1/60 s frame: how fast a crest below the threshold fills a texel.
var OceanFoam = struct {
	Tau, MaxThreshold, WindLow, WindHigh, Gain float64
}{Tau: 6, MaxThreshold: 0.64, WindLow: 5, WindHigh: 9, Gain: 0.9}

// OceanGraphHullNode is the logical node name the Elio ocean graph uses for the
// wake's hull; Ocean.GraphNodes maps it to Wake.NodeID.
const OceanGraphHullNode = "hull"

// Hull size defaults, the graph's own.
const (
	oceanDefaultHullLength = 200
	oceanDefaultHullBeam   = 30
)

// oceanGraphTweenSeconds is the default sea-state transition, the graph's own.
const oceanGraphTweenSeconds = 20

// oceanGraphParams returns the graph's value parameters for a normalized ocean
// that has a spectrum.
func oceanGraphParams(o *Ocean) map[string]float64 {
	p := oceanSpectrumParams(*o.Spectrum, o.WindDirection)
	choppiness := o.Choppiness
	if choppiness == 0 {
		choppiness = 0.6
	}
	timeScale, driftX, driftZ := 1.0, 0.0, 0.0
	if pr := o.Presentation; pr != nil {
		if pr.TimeScale != 0 {
			timeScale = pr.TimeScale
		}
		driftX, driftZ = pr.WaterDriftX, pr.WaterDriftZ
	}
	wakeEnabled, hullLength, hullBeam := 0.0, float64(oceanDefaultHullLength), float64(oceanDefaultHullBeam)
	if w := o.Wake; w != nil {
		if w.Enabled && w.NodeID != "" {
			wakeEnabled = 1
		}
		if w.Length > 0 {
			hullLength = w.Length
		}
		if w.Beam > 0 {
			hullBeam = w.Beam
		}
	}
	return map[string]float64{
		"wakeEnabled": wakeEnabled, "hullLength": hullLength, "hullBeam": hullBeam,
		"windSpeed": p.WindSpeed, "fetch": p.FetchKm, "gamma": p.Gamma, "spread": p.Spread, "swell": p.Swell,
		"windDirDeg": o.WindDirection, "swellDirDeg": o.Spectrum.SwellDirection,
		"seed": float64(p.Seed), "loopPeriod": 1000,
		"lambda":    choppiness,
		"timeScale": timeScale, "driftX": driftX, "driftZ": driftZ,
		"foamTau": OceanFoam.Tau, "foamThreshold": oceanFoamThreshold(p.WindSpeed, choppiness), "foamGain": OceanFoam.Gain,
		"probeCount": 0, "probeOriginX": 0, "probeOriginZ": 0,
	}
}

func oceanFoamThreshold(windSpeed, choppiness float64) float64 {
	t := math.Min(1, math.Max(0, (windSpeed-OceanFoam.WindLow)/(OceanFoam.WindHigh-OceanFoam.WindLow)))
	base := OceanFoam.MaxThreshold * t * t * (3 - 2*t)
	if base == 0 {
		return 0
	}
	return 1 - choppiness*(1-base)
}

func oceanGraphTween(o *Ocean) float64 {
	if o.Presentation != nil && o.Presentation.TransitionSeconds != 0 {
		return o.Presentation.TransitionSeconds
	}
	return oceanGraphTweenSeconds
}

// oceanGraphNodes binds the graph's hull node to the wake's scene node, or
// returns nil when no node is named.
func oceanGraphNodes(o *Ocean) map[string]string {
	if o.Wake == nil || o.Wake.NodeID == "" {
		return nil
	}
	return map[string]string{OceanGraphHullNode: o.Wake.NodeID}
}
