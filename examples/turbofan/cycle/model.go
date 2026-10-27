// Package cycle provides generic design-point and mapped fixed-geometry
// turbofan models. Both are steady component-cycle calculations.
package cycle

import (
	"fmt"
	"math"
)

const (
	Gamma = 1.4
	R     = 287.05 // J/(kg K)
	Cp    = Gamma * R / (Gamma - 1)
	TRef  = 288.15   // K
	PRef  = 101325.0 // Pa
)

// Inputs use SI units. OverallPressureRatio includes the fan, excludes the inlet.
// Altitude is geopotential altitude in the ISA troposphere, 0–11 km.
type Inputs struct {
	AltitudeM            float64 `json:"altitudeM"`
	Mach                 float64 `json:"mach"`
	TurbineInletK        float64 `json:"turbineInletK"`
	OverallPressureRatio float64 `json:"overallPressureRatio"`
	FanPressureRatio     float64 `json:"fanPressureRatio"`
	BypassRatio          float64 `json:"bypassRatio"`
}

func DefaultInputs() Inputs {
	return Inputs{TurbineInletK: 1600, OverallPressureRatio: 30, FanPressureRatio: 1.5, BypassRatio: 8}
}

// Model specifies design assumptions, rather than fits to a manufacturer's engine.
type Model struct {
	FanEfficiency        float64
	CompressorEfficiency float64
	TurbineEfficiency    float64
	ShaftEfficiency      float64
	BurnerEfficiency     float64
	InletRecovery        float64
	BurnerRecovery       float64
	FuelHeatingValue     float64 // J/kg, lower heating value
	CorrectedCoreFlow    float64 // kg/s at TRef, PRef, referenced to station 2
}

func DefaultModel() Model {
	return Model{FanEfficiency: .90, CompressorEfficiency: .88, TurbineEfficiency: .91,
		ShaftEfficiency: .99, BurnerEfficiency: .99, InletRecovery: .99,
		BurnerRecovery: .95, FuelHeatingValue: 43e6, CorrectedCoreFlow: 40}
}

// Station contains stagnation (total) conditions, except Ambient and nozzle exits.
type Station struct {
	TemperatureK float64 `json:"temperatureK"`
	PressurePa   float64 `json:"pressurePa"`
	MassFlowKGS  float64 `json:"massFlowKGS"`
}

// Nozzle is an ideal convergent nozzle sized to pass the specified mass flow.
// A choked nozzle retains pressure thrust; it is not expanded to ambient.
type Nozzle struct {
	Exit            Station `json:"exit"`
	Mach            float64 `json:"mach"`
	VelocityMPS     float64 `json:"velocityMPS"`
	AreaM2          float64 `json:"areaM2"`
	PressureThrustN float64 `json:"pressureThrustN"`
	Choked          bool    `json:"choked"`
	CriticalNPR     float64 `json:"criticalNPR"`
}

type Result struct {
	Inputs           Inputs  `json:"inputs"`
	Ambient          Station `json:"ambient"`
	Inlet            Station `json:"inlet"`
	Fan              Station `json:"fan"`
	Compressor       Station `json:"compressor"`
	Burner           Station `json:"burner"`
	HighTurbine      Station `json:"highTurbine"`
	LowTurbine       Station `json:"lowTurbine"`
	CoreNozzle       Nozzle  `json:"coreNozzle"`
	BypassNozzle     Nozzle  `json:"bypassNozzle"`
	FlightSpeedMPS   float64 `json:"flightSpeedMPS"`
	CoreFlowKGS      float64 `json:"coreFlowKGS"`
	BypassFlowKGS    float64 `json:"bypassFlowKGS"`
	FuelFlowKGS      float64 `json:"fuelFlowKGS"`
	FuelAirRatio     float64 `json:"fuelAirRatio"`
	CoreThrustN      float64 `json:"coreThrustN"`
	BypassThrustN    float64 `json:"bypassThrustN"`
	NetThrustN       float64 `json:"netThrustN"`
	TSFC             float64 `json:"tsfcKgPerNS"` // undefined when NetThrustN <= 0; stored as zero
	CompressorPowerW float64 `json:"compressorPowerW"`
	FanPowerW        float64 `json:"fanPowerW"`
	// Residuals are evaluated from component inlet/outlet states, in SI units.
	BurnerResidualW     float64 `json:"burnerResidualW"`
	HighShaftResidualW  float64 `json:"highShaftResidualW"`
	LowShaftResidualW   float64 `json:"lowShaftResidualW"`
	EnergyResidualW     float64 `json:"energyResidualW"`
	MassResidualKGS     float64 `json:"massResidualKGS"`
	MaxRelativeResidual float64 `json:"maxRelativeResidual"`
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func (m Model) validate(in Inputs) error {
	for _, item := range []struct {
		name            string
		value, min, max float64
	}{
		{"altitude", in.AltitudeM, 0, 11000}, {"Mach number", in.Mach, 0, .85},
		{"turbine inlet temperature", in.TurbineInletK, 1000, 1800},
		{"overall pressure ratio", in.OverallPressureRatio, 5, 40},
		{"fan pressure ratio", in.FanPressureRatio, 1.1, 1.8}, {"bypass ratio", in.BypassRatio, 1, 12},
	} {
		if !finite(item.value) || item.value < item.min || item.value > item.max {
			return fmt.Errorf("%s must be between %g and %g", item.name, item.min, item.max)
		}
	}
	for _, x := range []float64{m.FanEfficiency, m.CompressorEfficiency, m.TurbineEfficiency,
		m.ShaftEfficiency, m.BurnerEfficiency, m.InletRecovery, m.BurnerRecovery} {
		if !finite(x) || x <= 0 || x > 1 {
			return fmt.Errorf("efficiencies and pressure recoveries must be in (0, 1]")
		}
	}
	if !finite(m.CorrectedCoreFlow) || m.CorrectedCoreFlow <= 0 || !finite(m.FuelHeatingValue) ||
		m.FuelHeatingValue*m.BurnerEfficiency <= Cp*in.TurbineInletK {
		return fmt.Errorf("positive corrected airflow and sufficient fuel heating value are required")
	}
	return nil
}

// Solve balances each shaft algebraically. Every input change defines a new
// design point; nozzle areas are outputs and can change between points.
func (m Model) Solve(in Inputs) (Result, error) {
	r := Result{Inputs: in}
	if err := m.validate(in); err != nil {
		return r, err
	}
	t0 := TRef - .0065*in.AltitudeM
	p0 := PRef * math.Pow(t0/TRef, 9.80665/(R*.0065))
	r.Ambient = Station{TemperatureK: t0, PressurePa: p0}
	r.FlightSpeedMPS = in.Mach * math.Sqrt(Gamma*R*t0)
	ram := 1 + (Gamma-1)/2*in.Mach*in.Mach
	t2 := t0 * ram
	p2 := p0 * math.Pow(ram, Gamma/(Gamma-1)) * m.InletRecovery
	r.CoreFlowKGS = m.CorrectedCoreFlow * (p2 / PRef) / math.Sqrt(t2/TRef)
	r.BypassFlowKGS = r.CoreFlowKGS * in.BypassRatio
	totalFlow := r.CoreFlowKGS + r.BypassFlowKGS
	r.Inlet = Station{t2, p2, totalFlow}
	r.Fan = compress(r.Inlet, in.FanPressureRatio, m.FanEfficiency)
	coreIn := r.Fan
	coreIn.MassFlowKGS = r.CoreFlowKGS
	r.Compressor = compress(coreIn, in.OverallPressureRatio/in.FanPressureRatio, m.CompressorEfficiency)
	if in.TurbineInletK <= r.Compressor.TemperatureK {
		return r, fmt.Errorf("burner temperature must exceed compressor exit temperature; add heat or reduce pressure ratio")
	}
	r.FuelAirRatio = Cp * (in.TurbineInletK - r.Compressor.TemperatureK) / (m.BurnerEfficiency*m.FuelHeatingValue - Cp*in.TurbineInletK)
	r.FuelFlowKGS = r.FuelAirRatio * r.CoreFlowKGS
	hotFlow := r.CoreFlowKGS + r.FuelFlowKGS
	r.Burner = Station{in.TurbineInletK, r.Compressor.PressurePa * m.BurnerRecovery, hotFlow}
	r.CompressorPowerW = r.CoreFlowKGS * Cp * (r.Compressor.TemperatureK - r.Fan.TemperatureK)
	r.FanPowerW = totalFlow * Cp * (r.Fan.TemperatureK - t2)
	var err error
	r.HighTurbine, err = m.expandForLoad(r.Burner, r.CompressorPowerW)
	if err != nil {
		return r, err
	}
	r.LowTurbine, err = m.expandForLoad(r.HighTurbine, r.FanPowerW)
	if err != nil {
		return r, err
	}
	r.CoreNozzle, err = nozzle(r.LowTurbine, p0)
	if err != nil {
		return r, fmt.Errorf("core nozzle cannot discharge: reduce fan load/pressure ratio or increase burner temperature")
	}
	bypassIn := r.Fan
	bypassIn.MassFlowKGS = r.BypassFlowKGS
	r.BypassNozzle, err = nozzle(bypassIn, p0)
	if err != nil {
		return r, fmt.Errorf("bypass nozzle cannot discharge at these inlet and fan conditions")
	}
	r.CoreThrustN = hotFlow*r.CoreNozzle.VelocityMPS - r.CoreFlowKGS*r.FlightSpeedMPS + r.CoreNozzle.PressureThrustN
	r.BypassThrustN = r.BypassFlowKGS*(r.BypassNozzle.VelocityMPS-r.FlightSpeedMPS) + r.BypassNozzle.PressureThrustN
	r.NetThrustN = r.CoreThrustN + r.BypassThrustN
	if r.NetThrustN > 0 {
		r.TSFC = r.FuelFlowKGS / r.NetThrustN
	}
	r.BurnerResidualW = hotFlow*Cp*in.TurbineInletK - r.CoreFlowKGS*Cp*r.Compressor.TemperatureK - r.FuelFlowKGS*m.BurnerEfficiency*m.FuelHeatingValue
	r.HighShaftResidualW = m.ShaftEfficiency*hotFlow*Cp*(r.Burner.TemperatureK-r.HighTurbine.TemperatureK) - r.CompressorPowerW
	r.LowShaftResidualW = m.ShaftEfficiency*hotFlow*Cp*(r.HighTurbine.TemperatureK-r.LowTurbine.TemperatureK) - r.FanPowerW
	shaftLoss := (r.CompressorPowerW + r.FanPowerW) * (1/m.ShaftEfficiency - 1)
	r.EnergyResidualW = hotFlow*Cp*r.LowTurbine.TemperatureK + r.BypassFlowKGS*Cp*r.Fan.TemperatureK + shaftLoss - totalFlow*Cp*t2 - r.FuelFlowKGS*m.BurnerEfficiency*m.FuelHeatingValue
	r.MassResidualKGS = r.CoreNozzle.Exit.MassFlowKGS + r.BypassNozzle.Exit.MassFlowKGS - totalFlow - r.FuelFlowKGS
	for _, residual := range []float64{r.BurnerResidualW, r.HighShaftResidualW, r.LowShaftResidualW, r.EnergyResidualW} {
		r.MaxRelativeResidual = math.Max(r.MaxRelativeResidual, math.Abs(residual)/(r.FuelFlowKGS*m.FuelHeatingValue))
	}
	r.MaxRelativeResidual = math.Max(r.MaxRelativeResidual, math.Abs(r.MassResidualKGS)/totalFlow)
	if !finite(r.NetThrustN) || !finite(r.FuelFlowKGS) || !finite(r.MaxRelativeResidual) || r.MaxRelativeResidual > 1e-9 {
		return Result{Inputs: in}, fmt.Errorf("calculation exceeded numerical limits or failed a conservation check")
	}
	return r, nil
}

func compress(s Station, ratio, efficiency float64) Station {
	s.TemperatureK *= 1 + (math.Pow(ratio, (Gamma-1)/Gamma)-1)/efficiency
	s.PressurePa *= ratio
	return s
}

func (m Model) expandForLoad(s Station, load float64) (Station, error) {
	drop := load / (m.ShaftEfficiency * s.MassFlowKGS * Cp)
	isentropicRatio := 1 - drop/(m.TurbineEfficiency*s.TemperatureK)
	if isentropicRatio <= 0 {
		return Station{}, fmt.Errorf("turbine cannot supply the requested shaft power; reduce fan/compressor load")
	}
	s.TemperatureK -= drop
	s.PressurePa *= math.Pow(isentropicRatio, Gamma/(Gamma-1))
	return s, nil
}

func nozzle(in Station, ambientPa float64) (Nozzle, error) {
	if in.PressurePa <= ambientPa || in.TemperatureK <= 0 || in.MassFlowKGS <= 0 {
		return Nozzle{}, fmt.Errorf("positive nozzle pressure head, temperature and flow are required")
	}
	criticalRatio := math.Pow(2/(Gamma+1), Gamma/(Gamma-1))
	pExit := math.Max(ambientPa, in.PressurePa*criticalRatio)
	tExit := in.TemperatureK * math.Pow(pExit/in.PressurePa, (Gamma-1)/Gamma)
	vExit := math.Sqrt(2 * Cp * (in.TemperatureK - tExit))
	area := in.MassFlowKGS * R * tExit / (pExit * vExit)
	return Nozzle{Exit: Station{tExit, pExit, in.MassFlowKGS}, Mach: vExit / math.Sqrt(Gamma*R*tExit),
		VelocityMPS: vExit, AreaM2: area, PressureThrustN: (pExit - ambientPa) * area,
		Choked: ambientPa <= in.PressurePa*criticalRatio, CriticalNPR: 1 / criticalRatio}, nil
}
