package cycle

import (
	"fmt"
	"math"
	"sync"
)

type MappedInputs struct {
	AltitudeM   float64 `json:"altitudeM"`
	Mach        float64 `json:"mach"`
	FuelFlowKGS float64 `json:"fuelFlowKGS"`
}

type MappedResult struct {
	Cycle         Result       `json:"cycle"`
	Command       MappedInputs `json:"command"`
	LowCompressor Station      `json:"lowCompressor"`
	TurbineInlet  Station      `json:"turbineInlet"`
	N1RPM         float64      `json:"n1RPM"`
	N2RPM         float64      `json:"n2RPM"`
	MapPoints     []MapPoint   `json:"mapPoints"`
	MatchResidual float64      `json:"matchResidual"`
	Iterations    int          `json:"iterations"`
}

// MappedModel scales NASA map shapes at one reconstructed rated point. Its
// areas and map scalars remain fixed for all subsequent operating points.
// The public observations do not identify the proprietary component maps.
type MappedModel struct {
	LowSpeedLoss         float64             `json:"lowSpeedLoss"`
	IdleRecovery         float64             `json:"idleRecovery"`
	Scales               map[string]MapScale `json:"scales"`
	CoreAreaM2           float64             `json:"coreAreaM2"`
	BypassAreaM2         float64             `json:"bypassAreaM2"`
	ReferenceN1RPM       float64             `json:"referenceN1RPM"`
	ReferenceN2RPM       float64             `json:"referenceN2RPM"`
	RatedFlowKGS         float64             `json:"ratedFlowKGS"`
	Rated                Result              `json:"rated"`
	FanPressurePrior     float64             `json:"fanPressurePrior"`
	BoosterPressurePrior float64             `json:"boosterPressurePrior"`
	hpPower, lpPower     float64
}

// FitApproachLoss fits one common low-spool compressor efficiency correction
// to the approach observation. It never reads climbout or idle observations.
func FitApproachLoss(m *MappedModel) (float64, error) {
	var obs Observation
	for _, p := range reference.Points {
		if p.Mode == "approach" {
			obs = p
		}
	}
	target := obs.ThrustFraction * reference.RatedThrustN
	var previousLoss, previousError float64
	previousOK := false
	for loss := 0.0; loss <= 3.5; loss += .1 {
		m.LowSpeedLoss = loss
		r, err := m.Solve(MappedInputs{FuelFlowKGS: obs.FuelFlowKGS})
		if err != nil {
			previousOK = false
			continue
		}
		e := r.Cycle.NetThrustN - target
		if previousOK && e*previousError <= 0 {
			lo, hi := previousLoss, loss
			for range 35 {
				mid := (lo + hi) / 2
				m.LowSpeedLoss = mid
				r, err = m.Solve(MappedInputs{FuelFlowKGS: obs.FuelFlowKGS})
				if err != nil {
					return 0, err
				}
				if (r.Cycle.NetThrustN-target)*previousError > 0 {
					lo = mid
				} else {
					hi = mid
				}
			}
			m.LowSpeedLoss = (lo + hi) / 2
			return m.LowSpeedLoss, nil
		}
		previousLoss, previousError, previousOK = loss, e, true
	}
	m.LowSpeedLoss = 0
	return 0, fmt.Errorf("approach loss fit has no feasible bracket in [0,3.5]")
}

var mappedOnce sync.Once
var mappedDefault *MappedModel
var mappedErr error

func JT9D() (*MappedModel, error) {
	mappedOnce.Do(func() {
		mappedDefault, mappedErr = CalibrateJT9D()
		if mappedErr == nil {
			mappedDefault.LowSpeedLoss = profile.LowSpeedLoss
			mappedDefault.IdleRecovery = profile.IdleRecovery
		}
	})
	return mappedDefault, mappedErr
}

var designCoordinates = map[string]float64{"Fan": 2, "LPC": 2, "HPC": 2, "HPT": 5, "LPT": 6.4}

// CalibrateJT9D uses only the rated fuel/thrust point and published OPR/BPR.
// Efficiencies, map reference coordinates, cooling and shaft-speed references
// are declared NASA-model priors. No lower-thrust fuel observation is read.
func CalibrateJT9D() (*MappedModel, error) {
	m := &MappedModel{Scales: map[string]MapScale{}}
	eta := map[string]float64{}
	for name, r := range rawMaps {
		n := 1.0
		if r.Pressure == nil {
			n = 100
		}
		_, e, pr, err := r.lookup(n, designCoordinates[name])
		if err != nil {
			return nil, err
		}
		eta[name] = e * r.Scale["s_eff"]
		if name == "Fan" {
			m.FanPressurePrior = 1 + (pr-1)*r.Scale["s_PR"]
		}
		if name == "LPC" {
			m.BoosterPressurePrior = 1 + (pr-1)*r.Scale["s_PR"]
		}
	}
	fuel := reference.Points[0].FuelFlowKGS
	// The heat/work equations admit two airflow roots. Choose the root whose
	// core area is closest (in log space) to NASA's 862.88 in² geometry prior.
	bestScore := math.Inf(1)
	var rated designState
	var previous designState
	previousOK := false
	for flow := 200.0; flow <= 1600; flow += 5 {
		now, err := m.designPoint(flow, fuel, eta)
		if err == nil && previousOK && (now.result.NetThrustN-reference.RatedThrustN)*(previous.result.NetThrustN-reference.RatedThrustN) < 0 {
			lo, hi := previous.result.Inlet.MassFlowKGS, flow
			for range 50 {
				mid := (lo + hi) / 2
				s, e := m.designPoint(mid, fuel, eta)
				if e != nil {
					return nil, e
				}
				if (s.result.NetThrustN-reference.RatedThrustN)*(previous.result.NetThrustN-reference.RatedThrustN) > 0 {
					lo = mid
				} else {
					hi = mid
				}
			}
			s, e := m.designPoint((lo+hi)/2, fuel, eta)
			if e != nil {
				return nil, e
			}
			score := math.Abs(math.Log(s.result.CoreNozzle.AreaM2 / (862.88 * .00064516)))
			if score < bestScore {
				bestScore = score
				rated = s
			}
		}
		previousOK = err == nil
		previous = now
	}
	if !finite(bestScore) {
		return nil, fmt.Errorf("no rated-point airflow satisfies the declared priors")
	}
	m.Rated = rated.result
	m.RatedFlowKGS = rated.result.Inlet.MassFlowKGS
	m.CoreAreaM2 = rated.result.CoreNozzle.AreaM2
	m.BypassAreaM2 = rated.result.BypassNozzle.AreaM2
	m.ReferenceN1RPM = rawMaps["Fan"].Scale["s_Nc"]
	m.ReferenceN2RPM = rawMaps["HPC"].Scale["s_Nc"] * math.Sqrt(rated.booster.TemperatureK/TRef)
	m.hpPower = rated.result.CompressorPowerW
	m.lpPower = rated.result.FanPowerW
	for _, q := range []struct {
		name  string
		in    Station
		n, pr float64
	}{
		{"Fan", rated.result.Inlet, m.ReferenceN1RPM, m.FanPressurePrior},
		{"LPC", Station{rated.result.Fan.TemperatureK, rated.result.Fan.PressurePa, rated.result.CoreFlowKGS}, m.ReferenceN1RPM, m.BoosterPressurePrior},
		{"HPC", rated.booster, m.ReferenceN2RPM, reference.PressureRatio / m.FanPressurePrior / m.BoosterPressurePrior},
		{"HPT", rated.hptIn, m.ReferenceN2RPM, rated.hptPR}, {"LPT", rated.result.HighTurbine, m.ReferenceN1RPM, rated.lptPR},
	} {
		r := rawMaps[q.name]
		nc := 1.0
		if r.Pressure == nil {
			nc = 100
		}
		w, _, p, err := r.lookup(nc, designCoordinates[q.name])
		if err != nil {
			return nil, err
		}
		m.Scales[q.name] = MapScale{Speed: q.n / math.Sqrt(q.in.TemperatureK/TRef) / nc, Flow: q.in.MassFlowKGS * math.Sqrt(q.in.TemperatureK/TRef) / (q.in.PressurePa / PRef) / w, Pressure: (q.pr - 1) / (p - 1), Efficiency: r.Scale["s_eff"]}
	}
	return m, nil
}

type designState struct {
	result         Result
	booster, hptIn Station
	hptPR, lptPR   float64
}

func (m *MappedModel) designPoint(total, fuel float64, eta map[string]float64) (designState, error) {
	r := Result{CoreFlowKGS: total / (1 + reference.BypassRatio), FuelFlowKGS: fuel}
	r.BypassFlowKGS = total - r.CoreFlowKGS
	r.FuelAirRatio = fuel / (.91 * r.CoreFlowKGS)
	r.Ambient = Station{TemperatureK: TRef, PressurePa: PRef}
	r.Inlet = Station{TRef, PRef, total}
	var err error
	r.Fan, err = gasCompress(r.Inlet, m.FanPressurePrior, eta["Fan"])
	if err != nil {
		return designState{}, err
	}
	core := r.Fan
	core.MassFlowKGS = r.CoreFlowKGS
	booster, err := gasCompress(core, m.BoosterPressurePrior, eta["LPC"])
	if err != nil {
		return designState{}, err
	}
	r.Compressor, err = gasCompress(booster, reference.PressureRatio/m.FanPressurePrior/m.BoosterPressurePrior, eta["HPC"])
	if err != nil {
		return designState{}, err
	}
	r.Burner, err = burnCore(r.Compressor, fuel)
	if err != nil {
		return designState{}, err
	}
	hptIn, err := mixCooling(r.Burner, r.Compressor, .055*r.CoreFlowKGS)
	if err != nil {
		return designState{}, err
	}
	r.CompressorPowerW = r.CoreFlowKGS * (gasH(r.Compressor.TemperatureK) - gasH(booster.TemperatureK))
	hptOut, hpPR, err := expandLoad(hptIn, r.CompressorPowerW, eta["HPT"])
	if err != nil {
		return designState{}, err
	}
	r.HighTurbine, err = mixCooling(hptOut, r.Compressor, .035*r.CoreFlowKGS)
	if err != nil {
		return designState{}, err
	}
	r.FanPowerW = total*(gasH(r.Fan.TemperatureK)-gasH(TRef)) + r.CoreFlowKGS*(gasH(booster.TemperatureK)-gasH(r.Fan.TemperatureK))
	var lpPR float64
	r.LowTurbine, lpPR, err = expandLoad(r.HighTurbine, r.FanPowerW, eta["LPT"])
	if err != nil {
		return designState{}, err
	}
	r.CoreNozzle, err = gasNozzle(r.LowTurbine, PRef, 1)
	if err != nil {
		return designState{}, err
	}
	r.CoreNozzle, err = gasNozzle(r.LowTurbine, PRef, r.LowTurbine.MassFlowKGS/r.CoreNozzle.Exit.MassFlowKGS)
	if err != nil {
		return designState{}, err
	}
	bypass := r.Fan
	bypass.MassFlowKGS = r.BypassFlowKGS
	r.BypassNozzle, err = gasNozzle(bypass, PRef, 1)
	if err != nil {
		return designState{}, err
	}
	r.BypassNozzle, err = gasNozzle(bypass, PRef, r.BypassFlowKGS/r.BypassNozzle.Exit.MassFlowKGS)
	if err != nil {
		return designState{}, err
	}
	finishMapped(&r, hptIn, hptOut, booster)
	return designState{r, booster, hptIn, hpPR, lpPR}, nil
}

func burnCore(core Station, fuel float64) (Station, error) {
	flow := .91*core.MassFlowKGS + fuel
	t, err := gasInvert((.91*core.MassFlowKGS*gasH(core.TemperatureK)+.99*fuel*43e6)/flow, false)
	return Station{t, core.PressurePa * .945, flow}, err
}

func mixCooling(hot, cold Station, flow float64) (Station, error) {
	t, err := gasInvert((hot.MassFlowKGS*gasH(hot.TemperatureK)+flow*gasH(cold.TemperatureK))/(hot.MassFlowKGS+flow), false)
	return Station{t, hot.PressurePa, hot.MassFlowKGS + flow}, err
}

func expandLoad(in Station, power, eta float64) (Station, float64, error) {
	drop := power / (.99 * in.MassFlowKGS)
	t, err := gasInvert(gasH(in.TemperatureK)-drop, false)
	if err != nil {
		return Station{}, 0, err
	}
	ti, err := gasInvert(gasH(in.TemperatureK)-drop/eta, false)
	if err != nil {
		return Station{}, 0, err
	}
	pr := math.Exp((gasS(in.TemperatureK) - gasS(ti)) / R)
	return Station{t, in.PressurePa / pr, in.MassFlowKGS}, pr, nil
}

func finishMapped(r *Result, hptIn, hptOut, booster Station) {
	r.CoreThrustN = r.LowTurbine.MassFlowKGS*r.CoreNozzle.VelocityMPS - r.CoreFlowKGS*r.FlightSpeedMPS + r.CoreNozzle.PressureThrustN
	r.BypassThrustN = r.BypassFlowKGS*(r.BypassNozzle.VelocityMPS-r.FlightSpeedMPS) + r.BypassNozzle.PressureThrustN
	r.NetThrustN = r.CoreThrustN + r.BypassThrustN
	if r.NetThrustN > 0 {
		r.TSFC = r.FuelFlowKGS / r.NetThrustN
	}
	r.Inputs.TurbineInletK = r.Burner.TemperatureK
	r.Inputs.OverallPressureRatio = r.Compressor.PressurePa / r.Inlet.PressurePa
	r.Inputs.FanPressureRatio = r.Fan.PressurePa / r.Inlet.PressurePa
	r.Inputs.BypassRatio = r.BypassFlowKGS / r.CoreFlowKGS
	r.BurnerResidualW = r.Burner.MassFlowKGS*gasH(r.Burner.TemperatureK) - .91*r.CoreFlowKGS*gasH(r.Compressor.TemperatureK) - .99*r.FuelFlowKGS*43e6
	r.HighShaftResidualW = .99*hptIn.MassFlowKGS*(gasH(hptIn.TemperatureK)-gasH(hptOut.TemperatureK)) - r.CompressorPowerW
	r.LowShaftResidualW = .99*r.LowTurbine.MassFlowKGS*(gasH(r.HighTurbine.TemperatureK)-gasH(r.LowTurbine.TemperatureK)) - r.FanPowerW
	shaftLoss := (r.CompressorPowerW + r.FanPowerW) * (1/.99 - 1)
	r.EnergyResidualW = r.LowTurbine.MassFlowKGS*gasH(r.LowTurbine.TemperatureK) + r.BypassFlowKGS*gasH(r.Fan.TemperatureK) + shaftLoss - r.Inlet.MassFlowKGS*gasH(r.Inlet.TemperatureK) - .99*r.FuelFlowKGS*43e6
	r.MassResidualKGS = r.CoreNozzle.Exit.MassFlowKGS + r.BypassNozzle.Exit.MassFlowKGS - r.Inlet.MassFlowKGS - r.FuelFlowKGS
	for _, e := range []float64{r.BurnerResidualW, r.HighShaftResidualW, r.LowShaftResidualW, r.EnergyResidualW} {
		r.MaxRelativeResidual = math.Max(r.MaxRelativeResidual, math.Abs(e)/(r.FuelFlowKGS*43e6))
	}
	r.MaxRelativeResidual = math.Max(r.MaxRelativeResidual, math.Abs(r.MassResidualKGS)/r.Inlet.MassFlowKGS)
}

func (m *MappedModel) evaluate(x [7]float64, in MappedInputs) ([7]float64, MappedResult, error) {
	r := Result{Inputs: Inputs{AltitudeM: in.AltitudeM, Mach: in.Mach}, FuelFlowKGS: in.FuelFlowKGS}
	fail := func(err error) ([7]float64, MappedResult, error) { return [7]float64{}, MappedResult{}, err }
	ta := TRef - .0065*in.AltitudeM
	pa := PRef * math.Pow(ta/TRef, 9.80665/(R*.0065))
	v := in.Mach * math.Sqrt(gasCP(ta)/(gasCP(ta)-R)*R*ta)
	t2, err := gasInvert(gasH(ta)+v*v/2, false)
	if err != nil {
		return fail(err)
	}
	p2 := pa * math.Exp((gasS(t2)-gasS(ta))/R)
	r.Ambient = Station{TemperatureK: ta, PressurePa: pa}
	r.FlightSpeedMPS = v
	n1, n2 := x[0]*m.ReferenceN1RPM, x[1]*m.ReferenceN2RPM
	points := []MapPoint{}
	point := func(name string, n, t, c float64) (MapPoint, error) {
		p, e := m.point(name, n, t, c)
		if e == nil {
			points = append(points, p)
		}
		return p, e
	}
	fan, err := point("Fan", n1, t2, x[2])
	if err != nil {
		return fail(err)
	}
	total := fan.CorrectedFlowKGS * (p2 / PRef) / math.Sqrt(t2/TRef)
	r.Inlet = Station{t2, p2, total}
	r.Fan, err = gasCompress(r.Inlet, fan.PressureRatio, fan.Efficiency)
	if err != nil {
		return fail(err)
	}
	lpc, err := point("LPC", n1, r.Fan.TemperatureK, x[3])
	if err != nil {
		return fail(err)
	}
	r.CoreFlowKGS = lpc.CorrectedFlowKGS * (r.Fan.PressurePa / PRef) / math.Sqrt(r.Fan.TemperatureK/TRef)
	r.BypassFlowKGS = total - r.CoreFlowKGS
	if r.BypassFlowKGS <= 0 {
		return fail(fmt.Errorf("nonpositive bypass flow"))
	}
	core := r.Fan
	core.MassFlowKGS = r.CoreFlowKGS
	booster, err := gasCompress(core, lpc.PressureRatio, lpc.Efficiency)
	if err != nil {
		return fail(err)
	}
	hpc, err := point("HPC", n2, booster.TemperatureK, x[4])
	if err != nil {
		return fail(err)
	}
	hpcFlow := hpc.CorrectedFlowKGS * (booster.PressurePa / PRef) / math.Sqrt(booster.TemperatureK/TRef)
	r.Compressor, err = gasCompress(booster, hpc.PressureRatio, hpc.Efficiency)
	if err != nil {
		return fail(err)
	}
	r.Burner, err = burnCore(r.Compressor, in.FuelFlowKGS)
	if err != nil {
		return fail(err)
	}
	if r.Burner.TemperatureK <= r.Compressor.TemperatureK {
		return fail(fmt.Errorf("insufficient fuel for a burning cycle"))
	}
	hptIn, err := mixCooling(r.Burner, r.Compressor, .055*r.CoreFlowKGS)
	if err != nil {
		return fail(err)
	}
	hpt, err := point("HPT", n2, hptIn.TemperatureK, x[5])
	if err != nil {
		return fail(err)
	}
	hptFlow := hpt.CorrectedFlowKGS * (hptIn.PressurePa / PRef) / math.Sqrt(hptIn.TemperatureK/TRef)
	hptOut, err := gasExpand(hptIn, hpt.PressureRatio, hpt.Efficiency)
	if err != nil {
		return fail(err)
	}
	r.HighTurbine, err = mixCooling(hptOut, r.Compressor, .035*r.CoreFlowKGS)
	if err != nil {
		return fail(err)
	}
	lpt, err := point("LPT", n1, r.HighTurbine.TemperatureK, x[6])
	if err != nil {
		return fail(err)
	}
	lptFlow := lpt.CorrectedFlowKGS * (r.HighTurbine.PressurePa / PRef) / math.Sqrt(r.HighTurbine.TemperatureK/TRef)
	r.LowTurbine, err = gasExpand(r.HighTurbine, lpt.PressureRatio, lpt.Efficiency)
	if err != nil {
		return fail(err)
	}
	r.CoreNozzle, err = gasNozzle(r.LowTurbine, pa, m.CoreAreaM2)
	if err != nil {
		return fail(err)
	}
	bypass := r.Fan
	bypass.MassFlowKGS = r.BypassFlowKGS
	r.BypassNozzle, err = gasNozzle(bypass, pa, m.BypassAreaM2)
	if err != nil {
		return fail(err)
	}
	r.CompressorPowerW = r.CoreFlowKGS * (gasH(r.Compressor.TemperatureK) - gasH(booster.TemperatureK))
	r.FanPowerW = total*(gasH(r.Fan.TemperatureK)-gasH(t2)) + r.CoreFlowKGS*(gasH(booster.TemperatureK)-gasH(r.Fan.TemperatureK))
	r.FuelAirRatio = in.FuelFlowKGS / (.91 * r.CoreFlowKGS)
	finishMapped(&r, hptIn, hptOut, booster)
	errvec := [7]float64{(hpcFlow - r.CoreFlowKGS) / m.Rated.CoreFlowKGS, (hptFlow - hptIn.MassFlowKGS) / (m.Rated.CoreFlowKGS*.965 + reference.Points[0].FuelFlowKGS), r.HighShaftResidualW / m.hpPower, (lptFlow - r.LowTurbine.MassFlowKGS) / m.Rated.LowTurbine.MassFlowKGS, r.LowShaftResidualW / m.lpPower, (r.CoreNozzle.Exit.MassFlowKGS - r.LowTurbine.MassFlowKGS) / m.Rated.LowTurbine.MassFlowKGS, (r.BypassNozzle.Exit.MassFlowKGS - r.BypassFlowKGS) / m.Rated.BypassFlowKGS}
	res := 0.0
	for _, e := range errvec {
		if !finite(e) {
			return fail(fmt.Errorf("nonfinite matching residual"))
		}
		res = math.Max(res, math.Abs(e))
	}
	return errvec, MappedResult{Cycle: r, Command: in, LowCompressor: booster, TurbineInlet: hptIn, N1RPM: n1, N2RPM: n2, MapPoints: points, MatchResidual: res}, nil
}

func (m *MappedModel) Solve(in MappedInputs) (MappedResult, error) {
	if !finite(in.AltitudeM) || in.AltitudeM < 0 || in.AltitudeM > 11000 || !finite(in.Mach) || in.Mach < 0 || in.Mach > .85 || !finite(in.FuelFlowKGS) || in.FuelFlowKGS < .2054 || in.FuelFlowKGS > reference.Points[0].FuelFlowKGS {
		return MappedResult{}, fmt.Errorf("command outside altitude 0–11 km, Mach 0–0.85 or fuel 0.2054–2.055 kg/s")
	}
	x := [7]float64{1, 1, 2, 2, 2, 5, 6.4}
	ratedFuel := reference.Points[0].FuelFlowKGS
	steps := int(math.Ceil(math.Max(math.Abs(in.FuelFlowKGS-ratedFuel)/.075, math.Max(in.AltitudeM/1500, in.Mach/.1))))
	if steps < 1 {
		steps = 1
	}
	var result MappedResult
	totalIterations := 0
	for i := 1; i <= steps; i++ {
		f := float64(i) / float64(steps)
		command := MappedInputs{in.AltitudeM * f, in.Mach * f, ratedFuel + (in.FuelFlowKGS-ratedFuel)*f}
		var err error
		var iterations int
		x, result, iterations, err = m.match(x, command)
		totalIterations += iterations
		if err != nil {
			return MappedResult{}, fmt.Errorf("no converged point within the bounded map model at fuel %.4f kg/s: %w", in.FuelFlowKGS, err)
		}
	}
	result.Iterations = totalIterations
	return result, nil
}
